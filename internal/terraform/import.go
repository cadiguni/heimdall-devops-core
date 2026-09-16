package terraform

import (
	"context"
	"fmt"
	"strings"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

// CheckStatus é o resultado de uma verificação do preflight.
type CheckStatus string

const (
	// CheckOK: verificado e em ordem.
	CheckOK CheckStatus = "ok"

	// CheckWarn: vale saber, mas não impede o import.
	CheckWarn CheckStatus = "warn"

	// CheckFail: o import falharia ou faria a coisa errada. Bloqueia.
	CheckFail CheckStatus = "fail"
)

// Check é uma verificação de preflight, feita antes de mexer no state.
type Check struct {
	Name   string      `json:"name"`
	Status CheckStatus `json:"status"`
	Detail string      `json:"detail"`
}

// ImportPreflight é o que se sabe sobre um import antes de executá-lo.
type ImportPreflight struct {
	Address    string `json:"address"`
	ResourceID string `json:"resource_id"`

	// Container e Key dizem contra qual state o import aconteceria. São o dado
	// mais importante da saída: errar de ambiente é o acidente caro aqui.
	Container   string `json:"container"`
	Key         string `json:"key"`
	Environment string `json:"environment,omitempty"`

	Checks []Check `json:"checks"`

	// Commands são os comandos que seriam executados, com o backend já
	// preenchido, para conferência ou para rodar à mão.
	Commands []string `json:"commands"`
}

// Blocked informa se alguma verificação reprovou.
func (p *ImportPreflight) Blocked() bool {
	for _, c := range p.Checks {
		if c.Status == CheckFail {
			return true
		}
	}
	return false
}

func (p *ImportPreflight) add(name string, status CheckStatus, format string, args ...interface{}) {
	p.Checks = append(p.Checks, Check{
		Name:   name,
		Status: status,
		Detail: fmt.Sprintf(format, args...),
	})
}

// ImportRequest descreve o import pretendido.
type ImportRequest struct {
	Address    string
	ResourceID string

	// Account, Container e Key localizam o state no Azure.
	Account   string
	Container string
	Key       string

	// ContainerURL é o endereço completo do container, só para exibição.
	ContainerURL string
}

// StateClient é o que o preflight precisa do Azure: listar para ver o lock e
// baixar para ver o conteúdo.
type StateClient interface {
	azure.BlobLister
	azure.BlobDownloader
}

// PreflightImport verifica, sem escrever nada, se o import tem chance de dar
// certo e se é no state que a pessoa pensa que é.
//
// As checagens vêm dos modos de falha reais: state inexistente (backend
// errado), state travado (o import ia esperar e estourar) e endereço já
// ocupado (o recurso já está no state, então import é a operação errada).
func PreflightImport(ctx context.Context, client StateClient, req ImportRequest) (*ImportPreflight, error) {
	preflight := &ImportPreflight{
		Address:     req.Address,
		ResourceID:  req.ResourceID,
		Container:   req.ContainerURL,
		Key:         req.Key,
		Environment: environmentOf(req.Key),
		Checks:      []Check{},
		Commands:    importCommands(req),
	}

	if err := checkAddress(preflight, req.Address); err != nil {
		return nil, err
	}
	if err := checkResourceID(req.ResourceID); err != nil {
		return nil, err
	}

	entry, err := findState(ctx, client, req.Key)
	if err != nil {
		return nil, err
	}

	if entry == nil {
		preflight.add("state existe", CheckFail,
			"%s não existe no container — confira a chave do backend", req.Key)
		return preflight, nil
	}
	preflight.add("state existe", CheckOK, "%s, %d bytes", entry.Path, entry.SizeBytes)

	if entry.Locked() {
		detail := "outro processo está com o lock; o import ficaria esperando e estouraria"
		if entry.LockInfo != nil && entry.LockInfo.Who != "" {
			detail = fmt.Sprintf("travado por %s; o import ficaria esperando e estouraria", entry.LockInfo.Who)
		}
		preflight.add("state destravado", CheckFail, "%s", detail)
	} else {
		preflight.add("state destravado", CheckOK, "sem lease ativo")
	}

	inspection, err := InspectStateBlob(ctx, client, req.Key)
	if err != nil {
		return nil, err
	}

	checkAddressFree(preflight, inspection, req.Address)

	if inspection.Empty() {
		preflight.add("conteúdo do state", CheckWarn,
			"o state não rastreia recurso nenhum — confirme que é o state certo antes de importar")
	} else {
		preflight.add("conteúdo do state", CheckOK,
			"%d recursos gerenciados, %d data sources", inspection.Managed, inspection.Data)
	}

	return preflight, nil
}

// checkAddress recusa endereço evidentemente malformado antes de qualquer ida
// ao Azure.
func checkAddress(preflight *ImportPreflight, address string) error {
	if address == "" {
		return fmt.Errorf("endereço do recurso não informado")
	}
	if strings.HasPrefix(address, "data.") {
		return fmt.Errorf("não se importa data source: %s", address)
	}
	if !strings.Contains(address, ".") {
		return fmt.Errorf("endereço inválido %q: esperado algo como 'azurerm_resource_group.rg'", address)
	}
	return nil
}

// checkResourceID pega o ID estragado pelo shell antes de gastar uma ida ao
// Azure.
//
// Git Bash e MSYS convertem argumento que começa com "/" em caminho do Windows,
// e todo ID de recurso do Azure começa com "/subscriptions/". O resultado é um
// ID do tipo "C:/Program Files/Git/subscriptions/...", que o provider recusaria
// com uma mensagem que não explica nada.
func checkResourceID(id string) error {
	if id == "" {
		return fmt.Errorf("id do recurso não informado")
	}

	if !strings.HasPrefix(id, "/") &&
		(strings.Contains(id, "/subscriptions/") || strings.Contains(id, "/resourceGroups/")) {
		return fmt.Errorf("o id do recurso parece ter sido convertido pelo shell: %q\n"+
			"Git Bash e MSYS transformam argumento iniciado em \"/\" em caminho do Windows.\n"+
			"Contorne com MSYS_NO_PATHCONV=1 antes do comando, ou rode em PowerShell/cmd", id)
	}

	return nil
}

// checkAddressFree é a verificação mais útil: se o endereço já está no state, o
// import erra com "Resource already managed by Terraform" e a operação certa
// era outra.
func checkAddressFree(preflight *ImportPreflight, inspection *StateInspection, address string) {
	for _, r := range inspection.Resources {
		if r.Address == address {
			preflight.add("endereço livre", CheckFail,
				"%s já está no state — import é a operação errada aqui", address)
			return
		}
	}
	preflight.add("endereço livre", CheckOK, "%s não está no state", address)
}

// findState localiza o state pela chave exata dentro do container.
func findState(ctx context.Context, lister azure.BlobLister, key string) (*StateEntry, error) {
	inventory, err := ListStates(ctx, lister, ListStatesOptions{Prefix: key, IncludeNonState: true})
	if err != nil {
		return nil, err
	}

	for _, s := range inventory.States {
		if s.Path == key {
			return &s, nil
		}
	}
	return nil, nil
}

// importCommands monta os comandos equivalentes ao que o heimdall faria, para
// a pessoa conferir ou rodar à mão.
//
// O 'init' é mostrado, nunca executado: reconfigurar o backend de um diretório
// de trabalho alheio pode migrar state, e isso não cabe num comando cuja
// proposta é ser seguro.
func importCommands(req ImportRequest) []string {
	return []string{
		fmt.Sprintf("terraform init -backend-config='storage_account_name=%s' "+
			"-backend-config='container_name=%s' -backend-config='key=%s' -backend-config='use_azuread_auth=true'",
			req.Account, req.Container, req.Key),
		fmt.Sprintf("terraform import '%s' '%s'", req.Address, req.ResourceID),
	}
}
