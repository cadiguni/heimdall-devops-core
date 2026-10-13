package terraform

import (
	"context"
	"fmt"
	"strings"
)

// RemapCategory classifica cada recurso do state diante de uma troca de
// assinatura.
//
// São três e não duas de propósito: o terceiro grupo é o perigoso, e some se
// for tratado como "não se aplica".
type RemapCategory string

const (
	// RemapTarget: o ID tem a assinatura de origem no caminho e pode ser
	// reescrito. É o caso que o comando resolve.
	RemapTarget RemapCategory = "remap"

	// RemapUntouched: o ID não menciona a assinatura de origem. ID de storage
	// container é URL, ID de recurso filho às vezes é só o nome — nenhum deles
	// precisa mudar quando a assinatura muda.
	RemapUntouched RemapCategory = "untouched"

	// RemapOpaque: a assinatura de origem aparece no ID, mas não como segmento
	// de caminho — base64, querystring, formato do provedor. Reescrever às
	// cegas produziria um ID inválido, e ignorar deixaria a migração pela
	// metade em silêncio. Então para e pede atenção.
	RemapOpaque RemapCategory = "opaque"
)

// RemapEntry é um recurso do state diante da troca.
type RemapEntry struct {
	Address  string        `json:"address"`
	Mode     string        `json:"mode"`
	Category RemapCategory `json:"category"`

	// CurrentID e NewID só são preenchidos na categoria remap.
	CurrentID string `json:"current_id,omitempty"`
	NewID     string `json:"new_id,omitempty"`

	// Reason explica a categoria quando ela não é remap.
	Reason string `json:"reason,omitempty"`
}

// RemapRequest descreve a troca pretendida.
type RemapRequest struct {
	From string
	To   string

	// ResourceGroup estreita a troca a um resource group. Um move costuma ser
	// de um RG inteiro, e sem esse filtro a troca atingiria recursos da mesma
	// assinatura que não foram movidos — apontando o state para IDs que não
	// existem.
	ResourceGroup string

	Account   string
	Container string
	Key       string

	ContainerURL string

	// Module é o retrato do diretório de trabalho, quando houver. Sem ele as
	// verificações de configuração não acontecem, e o --apply é recusado antes
	// de qualquer coisa.
	Module *ModuleSnapshot
}

// RemapPlan é o que aconteceria, antes de acontecer.
type RemapPlan struct {
	From string `json:"from"`
	To   string `json:"to"`

	ResourceGroup string `json:"resource_group,omitempty"`
	Container     string `json:"container"`
	Key           string `json:"key"`
	Environment   string `json:"environment,omitempty"`
	ModuleDir     string `json:"module_dir,omitempty"`

	Checks  []Check      `json:"checks"`
	Entries []RemapEntry `json:"entries"`
}

// Blocked informa se alguma verificação reprovou.
func (p *RemapPlan) Blocked() bool {
	for _, c := range p.Checks {
		if c.Status == CheckFail {
			return true
		}
	}
	return false
}

// Targets são os recursos que serão remapeados.
func (p *RemapPlan) Targets() []RemapEntry { return p.byCategory(RemapTarget) }

// Opaque são os que mencionam a assinatura em formato não reescrevível.
func (p *RemapPlan) Opaque() []RemapEntry { return p.byCategory(RemapOpaque) }

// Untouched são os que não mencionam a assinatura de origem.
func (p *RemapPlan) Untouched() []RemapEntry { return p.byCategory(RemapUntouched) }

func (p *RemapPlan) byCategory(c RemapCategory) []RemapEntry {
	var out []RemapEntry
	for _, e := range p.Entries {
		if e.Category == c {
			out = append(out, e)
		}
	}
	return out
}

func (p *RemapPlan) add(name string, status CheckStatus, format string, args ...interface{}) {
	p.Checks = append(p.Checks, Check{
		Name:   name,
		Status: status,
		Detail: fmt.Sprintf(format, args...),
	})
}

// PlanRemap monta o plano de troca de assinatura, sem escrever nada.
func PlanRemap(ctx context.Context, client StateClient, req RemapRequest) (*RemapPlan, error) {
	if err := ValidateRemap(req.From, req.To); err != nil {
		return nil, err
	}

	plan := &RemapPlan{
		From:          req.From,
		To:            req.To,
		ResourceGroup: req.ResourceGroup,
		Container:     req.ContainerURL,
		Key:           req.Key,
		Environment:   environmentOf(req.Key),
		Checks:        []Check{},
		Entries:       []RemapEntry{},
	}

	// O storage account do backend não pode estar na assinatura que está
	// saindo: se estiver, o próprio state desaparece no meio da operação e não
	// sobra nada para corrigir depois.
	checkBackendOutOfScope(plan, req)

	entry, err := findState(ctx, client, req.Key)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		plan.add("state existe", CheckFail,
			"%s não existe no container — confira a chave do backend", req.Key)
		return plan, nil
	}
	plan.add("state existe", CheckOK, "%s, %d bytes", entry.Path, entry.SizeBytes)

	if entry.Locked() {
		plan.add("state destravado", CheckFail, "tem lease ativo; a operação esperaria e estouraria")
	} else {
		plan.add("state destravado", CheckOK, "sem lease ativo")
	}

	inspection, err := InspectStateBlob(ctx, client, req.Key)
	if err != nil {
		return nil, err
	}

	plan.Entries = classifyForRemap(inspection.Resources, req)
	checkTargets(plan)
	checkModule(plan, req.Module, inspection.Lineage, req.From)

	return plan, nil
}

// ValidateRemap recusa o par de assinaturas antes de qualquer I/O.
func ValidateRemap(from, to string) error {
	if from == "" || to == "" {
		return fmt.Errorf("informe a assinatura de origem (--from) e a de destino (--to)")
	}
	if strings.EqualFold(from, to) {
		return fmt.Errorf("origem e destino são a mesma assinatura: %s", from)
	}
	return nil
}

// checkBackendOutOfScope é a verificação que não pode faltar.
//
// O heimdall não tem como saber em qual assinatura o storage account do backend
// mora — isso é control plane, e quem roda o comando pode ter só acesso de
// dados. Então em vez de afirmar, avisa: quem conhece o ambiente confirma.
func checkBackendOutOfScope(plan *RemapPlan, req RemapRequest) {
	plan.add("backend fora do escopo", CheckWarn,
		"confirme que o storage account %s NÃO está na assinatura %s. "+
			"Se estiver, o state desaparece no meio da operação e não há como corrigir depois",
		req.Account, req.From)
}

// classifyForRemap separa os recursos nas três categorias.
func classifyForRemap(resources []StateResource, req RemapRequest) []RemapEntry {
	prefixo := "/subscriptions/" + req.From
	entries := make([]RemapEntry, 0, len(resources))

	for _, r := range resources {
		entry := RemapEntry{Address: r.Address, Mode: r.Mode, CurrentID: r.ID}

		switch {
		// Data source relê a cada plan e se corrige sozinho; e não se importa
		// data source de qualquer forma.
		case r.Mode == "data":
			entry.Category = RemapUntouched
			entry.Reason = "data source: relido a cada plan, se corrige sozinho"

		case r.ID == "":
			entry.Category = RemapUntouched
			entry.Reason = "sem id no state"

		case containsFold(r.ID, prefixo):
			if req.ResourceGroup != "" && !containsFold(r.ID, "/resourceGroups/"+req.ResourceGroup) {
				entry.Category = RemapUntouched
				entry.Reason = "fora do resource group " + req.ResourceGroup
				break
			}
			entry.Category = RemapTarget
			entry.NewID = replaceFold(r.ID, req.From, req.To)

		// A assinatura aparece, mas não como segmento de caminho. É o caso do
		// id em base64 que apareceu num state real: reescrever às cegas daria
		// um id inválido.
		case containsFold(r.ID, req.From):
			entry.Category = RemapOpaque
			entry.Reason = "a assinatura aparece no id, mas não como caminho — formato do provedor"

		default:
			entry.Category = RemapUntouched
			entry.Reason = "o id não menciona a assinatura de origem"
		}

		entries = append(entries, entry)
	}

	return entries
}

func checkTargets(plan *RemapPlan) {
	alvos := len(plan.Targets())
	opacos := len(plan.Opaque())

	if alvos == 0 {
		plan.add("recursos a remapear", CheckFail,
			"nenhum recurso do state tem a assinatura de origem no id — confira o --from")
	} else {
		plan.add("recursos a remapear", CheckOK, "%d de %d", alvos, len(plan.Entries))
	}

	if opacos > 0 {
		plan.add("ids não reescrevíveis", CheckFail,
			"%d recurso(s) mencionam a assinatura em formato que este comando não reescreve; "+
				"corrija-os antes, senão a migração fica pela metade", opacos)
	}
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

func replaceFold(s, old, new string) string {
	i := strings.Index(strings.ToLower(s), strings.ToLower(old))
	if i < 0 {
		return s
	}
	return s[:i] + new + replaceFold(s[i+len(old):], old, new)
}
