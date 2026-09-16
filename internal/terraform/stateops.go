package terraform

import (
	"context"
	"fmt"
	"strings"
)

// StateOp é a operação de state pretendida.
type StateOp string

const (
	// StateOpRemove tira o recurso do state sem destruir a infraestrutura.
	StateOpRemove StateOp = "rm"

	// StateOpMove troca o endereço de um recurso dentro do mesmo state.
	StateOpMove StateOp = "mv"
)

// StateOpPreflight é o que se sabe sobre um rm ou mv antes de executá-lo.
type StateOpPreflight struct {
	Operation StateOp `json:"operation"`

	// Address é o endereço afetado. Destination só vale para mv.
	Address     string `json:"address"`
	Destination string `json:"destination,omitempty"`

	Container   string `json:"container"`
	Key         string `json:"key"`
	Environment string `json:"environment,omitempty"`

	Checks []Check `json:"checks"`

	// Affected lista o que sai do state (rm) ou o que muda de endereço (mv).
	// Um endereço sem índice atinge todas as instâncias de um count/for_each,
	// e é isso que precisa estar visível antes de confirmar.
	Affected []StateResource `json:"affected,omitempty"`

	Commands []string `json:"commands"`
}

// Blocked informa se alguma verificação reprovou.
func (p *StateOpPreflight) Blocked() bool {
	for _, c := range p.Checks {
		if c.Status == CheckFail {
			return true
		}
	}
	return false
}

func (p *StateOpPreflight) add(name string, status CheckStatus, format string, args ...interface{}) {
	p.Checks = append(p.Checks, Check{
		Name:   name,
		Status: status,
		Detail: fmt.Sprintf(format, args...),
	})
}

// StateOpRequest descreve a operação pretendida.
type StateOpRequest struct {
	Operation   StateOp
	Address     string
	Destination string

	Account   string
	Container string
	Key       string

	ContainerURL string
}

// PreflightStateOp verifica, sem escrever nada, se o rm ou o mv faz sentido no
// state indicado.
//
// As checagens são as do import ao contrário: lá o endereço precisa estar
// livre, aqui ele precisa existir. E no mv o destino é que precisa estar livre.
func PreflightStateOp(ctx context.Context, client StateClient, req StateOpRequest) (*StateOpPreflight, error) {
	if err := validateStateOp(req); err != nil {
		return nil, err
	}

	preflight := &StateOpPreflight{
		Operation:   req.Operation,
		Address:     req.Address,
		Destination: req.Destination,
		Container:   req.ContainerURL,
		Key:         req.Key,
		Environment: environmentOf(req.Key),
		Checks:      []Check{},
		Commands:    stateOpCommands(req),
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
		detail := "outro processo está com o lock; a operação ficaria esperando e estouraria"
		if entry.LockInfo != nil && entry.LockInfo.Who != "" {
			detail = fmt.Sprintf("travado por %s; a operação ficaria esperando e estouraria", entry.LockInfo.Who)
		}
		preflight.add("state destravado", CheckFail, "%s", detail)
	} else {
		preflight.add("state destravado", CheckOK, "sem lease ativo")
	}

	inspection, err := InspectStateBlob(ctx, client, req.Key)
	if err != nil {
		return nil, err
	}

	preflight.Affected = matchAddress(inspection, req.Address)
	checkAddressPresent(preflight, req.Address)

	if req.Operation == StateOpMove {
		checkDestinationFree(preflight, inspection, req.Destination)
	}

	return preflight, nil
}

func validateStateOp(req StateOpRequest) error {
	if req.Operation != StateOpRemove && req.Operation != StateOpMove {
		return fmt.Errorf("operação de state desconhecida: %q", req.Operation)
	}
	if req.Address == "" {
		return fmt.Errorf("endereço do recurso não informado")
	}
	if !strings.Contains(req.Address, ".") {
		return fmt.Errorf("endereço inválido %q: esperado algo como 'azurerm_resource_group.rg'", req.Address)
	}

	if req.Operation != StateOpMove {
		return nil
	}

	if req.Destination == "" {
		return fmt.Errorf("endereço de destino não informado")
	}
	if !strings.Contains(req.Destination, ".") {
		return fmt.Errorf("endereço de destino inválido %q", req.Destination)
	}
	if req.Destination == req.Address {
		return fmt.Errorf("origem e destino são o mesmo endereço: %s", req.Address)
	}

	return nil
}

// matchAddress devolve as instâncias atingidas pelo endereço.
//
// Endereço sem índice atinge todas as instâncias de um count/for_each: pedir
// 'rm azurerm_subnet.sub' quando existem sub["app"] e sub["db"] tira as duas.
// É por isso que a lista vai para a saída antes de qualquer confirmação.
func matchAddress(inspection *StateInspection, address string) []StateResource {
	var affected []StateResource

	for _, r := range inspection.Resources {
		if r.Address == address || strings.HasPrefix(r.Address, address+"[") {
			affected = append(affected, r)
		}
	}
	return affected
}

// checkAddressPresent é o espelho da verificação do import: aqui o endereço
// precisa existir, senão não há o que remover ou mover.
func checkAddressPresent(preflight *StateOpPreflight, address string) {
	if len(preflight.Affected) == 0 {
		preflight.add("endereço existe", CheckFail,
			"%s não está no state — nada a fazer", address)
		return
	}

	if len(preflight.Affected) > 1 {
		preflight.add("endereço existe", CheckWarn,
			"%s atinge %d instâncias (count/for_each); todas entram na operação",
			address, len(preflight.Affected))
		return
	}

	preflight.add("endereço existe", CheckOK, "%s está no state", preflight.Affected[0].Address)
}

// checkDestinationFree evita sobrescrever um recurso já rastreado: o mv para um
// endereço ocupado substituiria o que está lá.
func checkDestinationFree(preflight *StateOpPreflight, inspection *StateInspection, destination string) {
	if len(matchAddress(inspection, destination)) > 0 {
		preflight.add("destino livre", CheckFail,
			"%s já está no state — o mv sobrescreveria o que está lá", destination)
		return
	}
	preflight.add("destino livre", CheckOK, "%s não está no state", destination)
}

func stateOpCommands(req StateOpRequest) []string {
	init := fmt.Sprintf("terraform init -backend-config='storage_account_name=%s' "+
		"-backend-config='container_name=%s' -backend-config='key=%s' -backend-config='use_azuread_auth=true'",
		req.Account, req.Container, req.Key)

	if req.Operation == StateOpMove {
		return []string{init, fmt.Sprintf("terraform state mv '%s' '%s'", req.Address, req.Destination)}
	}
	return []string{init, fmt.Sprintf("terraform state rm '%s'", req.Address)}
}
