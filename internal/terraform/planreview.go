package terraform

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	tfjson "github.com/hashicorp/terraform-json"
)

// ChangeKind é a operação planejada para um recurso, já normalizada.
//
// O formato JSON do Terraform representa a operação como uma lista de ações
// (replace é ["delete","create"] ou ["create","delete"]); aqui isso vira um
// único valor, que é o que interessa para revisão e para o gate de pipeline.
type ChangeKind string

const (
	KindNoOp    ChangeKind = "no-op"
	KindCreate  ChangeKind = "create"
	KindRead    ChangeKind = "read"
	KindUpdate  ChangeKind = "update"
	KindDelete  ChangeKind = "delete"
	KindReplace ChangeKind = "replace"
	KindForget  ChangeKind = "forget"
)

// Destructive informa se a operação destrói infraestrutura ou remove o recurso
// do state.
//
// KindForget entra aqui de propósito: a infraestrutura sobrevive, mas o recurso
// sai do state, e perder rastreio de um recurso é exatamente o tipo de mudança
// que precisa de revisão humana.
func (k ChangeKind) Destructive() bool {
	switch k {
	case KindDelete, KindReplace, KindForget:
		return true
	default:
		return false
	}
}

// ResourceChange é a mudança planejada para um recurso, reduzida ao que é
// seguro exibir.
//
// Nenhum valor de atributo é copiado do plano: só endereço, tipo e a natureza
// da operação. É o que garante que um plano contendo secrets (senhas, chaves,
// referências de Key Vault) não vaze pela saída do heimdall.
type ResourceChange struct {
	Address      string     `json:"address"`
	Type         string     `json:"type"`
	Name         string     `json:"name"`
	ProviderName string     `json:"provider_name,omitempty"`
	Kind         ChangeKind `json:"kind"`

	// CreateBeforeDestroy só é significativo quando Kind == KindReplace.
	CreateBeforeDestroy bool `json:"create_before_destroy,omitempty"`

	// Reason é a explicação do Terraform para a ação, quando ele informa uma
	// (recurso tainted, replace explícito, config removida...). Vazio quando o
	// plano não traz motivo.
	Reason string `json:"reason,omitempty"`
}

// Summary conta as mudanças por operação, considerando apenas recursos
// gerenciados — mesma convenção do "Plan: X to add, Y to change, Z to destroy"
// do próprio Terraform.
type Summary struct {
	Create  int `json:"create"`
	Update  int `json:"update"`
	Delete  int `json:"delete"`
	Replace int `json:"replace"`
	Forget  int `json:"forget"`
	NoOp    int `json:"no_op"`

	// DataReads conta data sources que serão lidos durante o apply. Não são
	// mudanças de infraestrutura, ficam separados para não inflar o resumo.
	DataReads int `json:"data_reads"`
}

// Changed é o total de recursos gerenciados que o plano altera de alguma forma.
func (s Summary) Changed() int {
	return s.Create + s.Update + s.Delete + s.Replace + s.Forget
}

// PlanReview é o resultado da revisão de um plano.
type PlanReview struct {
	TerraformVersion string  `json:"terraform_version,omitempty"`
	FormatVersion    string  `json:"format_version,omitempty"`
	Summary          Summary `json:"summary"`

	// Destructive lista, ordenado por endereço, todo recurso que será
	// destruído, recriado ou removido do state.
	Destructive []ResourceChange `json:"destructive"`

	// Incomplete indica que o Terraform não conseguiu planejar tudo (uso de
	// -target, ou mudanças adiadas). Um plano incompleto não serve como gate:
	// o que ficou de fora pode conter destruição.
	//
	// É nil em planos gerados por Terraform anterior a 1.8, que não reportam
	// esse campo.
	Incomplete *bool `json:"incomplete,omitempty"`
}

// HasDestructive informa se o plano destrói ou recria algo.
func (r *PlanReview) HasDestructive() bool {
	return len(r.Destructive) > 0
}

// IsIncomplete informa se o plano comprovadamente não cobre toda a
// configuração.
//
// Retorna false quando o Terraform não informou o campo (versões anteriores à
// 1.8): "não sei" não é o mesmo que "está incompleto", e reprovar por
// desconhecimento inutilizaria a revisão nessas versões.
func (r *PlanReview) IsIncomplete() bool {
	return r.Incomplete != nil && *r.Incomplete
}

// ParsePlanJSON lê a saída de `terraform show -json <planfile>`.
//
// A validação da versão de formato é feita pelo próprio terraform-json ao
// desserializar, então um JSON que não seja um plano é rejeitado aqui.
func ParsePlanJSON(r io.Reader) (*tfjson.Plan, error) {
	var plan tfjson.Plan
	if err := json.NewDecoder(r).Decode(&plan); err != nil {
		return nil, fmt.Errorf("plano JSON inválido: %w", err)
	}
	return &plan, nil
}

// ReviewPlan classifica as mudanças de um plano já desserializado.
//
// É uma função pura: não lê arquivos, não executa terraform e não escreve na
// saída — para poder ser reaproveitada por um futuro servidor HTTP.
func ReviewPlan(plan *tfjson.Plan) *PlanReview {
	review := &PlanReview{
		TerraformVersion: plan.TerraformVersion,
		FormatVersion:    plan.FormatVersion,
		Incomplete:       incompleteFlag(plan),
		Destructive:      []ResourceChange{},
	}

	for _, rc := range plan.ResourceChanges {
		if rc == nil || rc.Change == nil {
			continue
		}

		if rc.Mode == tfjson.DataResourceMode {
			// Data sources não mudam infraestrutura; só interessa saber que
			// existe leitura pendente para o apply.
			if !rc.Change.Actions.NoOp() {
				review.Summary.DataReads++
			}
			continue
		}

		kind, known := classify(rc.Change.Actions)
		if !known {
			// Combinação de ações desconhecida (formato mais novo que este
			// código): ignorar em silêncio poderia esconder uma destruição,
			// então classificamos como replace para cair na revisão.
			kind = KindReplace
		}

		switch kind {
		case KindCreate:
			review.Summary.Create++
		case KindUpdate:
			review.Summary.Update++
		case KindDelete:
			review.Summary.Delete++
		case KindReplace:
			review.Summary.Replace++
		case KindForget:
			review.Summary.Forget++
		case KindNoOp:
			review.Summary.NoOp++
		case KindRead:
			review.Summary.DataReads++
		}

		if kind.Destructive() {
			review.Destructive = append(review.Destructive, ResourceChange{
				Address:             rc.Address,
				Type:                rc.Type,
				Name:                rc.Name,
				ProviderName:        rc.ProviderName,
				Kind:                kind,
				CreateBeforeDestroy: kind == KindReplace && rc.Change.Actions.CreateBeforeDestroy(),
				Reason:              reasonText(rc.ActionReason),
			})
		}
	}

	sort.Slice(review.Destructive, func(i, j int) bool {
		return review.Destructive[i].Address < review.Destructive[j].Address
	})

	return review
}

// classify reduz a lista de ações do plano a um único ChangeKind. O segundo
// retorno é false para combinações que este código não conhece.
func classify(actions tfjson.Actions) (ChangeKind, bool) {
	switch {
	case actions.NoOp():
		return KindNoOp, true
	case actions.Create():
		return KindCreate, true
	case actions.Update():
		return KindUpdate, true
	case actions.Delete():
		return KindDelete, true
	case actions.Replace():
		return KindReplace, true
	case actions.Forget():
		return KindForget, true
	case actions.Read():
		return KindRead, true
	default:
		return "", false
	}
}

func incompleteFlag(plan *tfjson.Plan) *bool {
	if plan.Complete == nil {
		return nil
	}
	incomplete := !*plan.Complete
	return &incomplete
}

// reasonText traduz o action_reason do plano para uma frase curta. Motivos
// desconhecidos são repassados crus em vez de descartados.
func reasonText(reason tfjson.ActionReason) string {
	switch reason {
	case tfjson.ActionReasonNone:
		return ""
	case tfjson.ActionReasonReplaceBecauseCannotUpdate:
		return "atributo alterado não suporta update in-place"
	case tfjson.ActionReasonReplaceBecauseTainted:
		return "recurso marcado como tainted"
	case tfjson.ActionReasonReplaceByRequest:
		return "replace pedido explicitamente (-replace)"
	case tfjson.ActionReasonReplaceByTriggers:
		return "replace_triggered_by disparado"
	case tfjson.ActionReasonDeleteBecauseNoResourceConfig:
		return "sem bloco de configuração correspondente"
	case tfjson.ActionReasonDeleteBecauseWrongRepetition:
		return "chave da instância incompatível com count/for_each"
	case tfjson.ActionReasonDeleteBecauseCountIndex:
		return "índice fora do count atual"
	case tfjson.ActionReasonDeleteBecauseEachKey:
		return "chave ausente no for_each atual"
	case tfjson.ActionReasonDeleteBecauseNoModule:
		return "módulo que continha o recurso não é mais declarado"
	case tfjson.ActionReasonDeleteBecauseNoMoveTarget:
		return "movido para um endereço sem configuração"
	default:
		return string(reason)
	}
}
