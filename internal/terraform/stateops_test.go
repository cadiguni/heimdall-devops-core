package terraform

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

// stateComCount tem um recurso simples e outro com for_each de duas chaves: é
// o caso que decide se um endereço sem índice atinge uma ou várias instâncias.
const stateComCount = `{
  "version": 4, "terraform_version": "1.9.8", "serial": 12, "lineage": "abc",
  "outputs": {},
  "resources": [
    {"mode":"managed","type":"azurerm_storage_account","name":"stg","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{}]},
    {"mode":"managed","type":"azurerm_subnet","name":"sub","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{"index_key":"app"},{"index_key":"db"}]}
  ]
}`

func clienteComState(conteudo string, lease string) *fakeStateClient {
	return &fakeStateClient{
		blobs:    []azure.Blob{{Name: "dev/app.tfstate", SizeBytes: 4096, LeaseState: lease}},
		conteudo: conteudo,
	}
}

func pedidoOp(op StateOp, address, destination string) StateOpRequest {
	return StateOpRequest{
		Operation:    op,
		Address:      address,
		Destination:  destination,
		Account:      "stterraform",
		Container:    "time1",
		Key:          "dev/app.tfstate",
		ContainerURL: "https://stterraform.blob.core.windows.net/time1",
	}
}

func preflightOp(t *testing.T, client StateClient, req StateOpRequest) *StateOpPreflight {
	t.Helper()

	p, err := PreflightStateOp(context.Background(), client, req)
	if err != nil {
		t.Fatalf("PreflightStateOp: %v", err)
	}
	return p
}

func statusOp(p *StateOpPreflight, name string) CheckStatus {
	for _, c := range p.Checks {
		if c.Name == name {
			return c.Status
		}
	}
	return ""
}

func TestPreflightRemoveCaminhoFeliz(t *testing.T) {
	p := preflightOp(t, clienteComState(stateComCount, "available"),
		pedidoOp(StateOpRemove, "azurerm_storage_account.stg", ""))

	if p.Blocked() {
		t.Fatalf("bloqueado sem motivo: %+v", p.Checks)
	}
	if statusOp(p, "endereço existe") != CheckOK {
		t.Errorf("endereço existe = %q", statusOp(p, "endereço existe"))
	}
	if len(p.Affected) != 1 || p.Affected[0].Address != "azurerm_storage_account.stg" {
		t.Errorf("Affected = %+v", p.Affected)
	}
	// O rm não tem verificação de destino.
	if statusOp(p, "destino livre") != "" {
		t.Error("rm não deveria ter verificação de destino")
	}
}

// O espelho do import: aqui o endereço precisa existir.
func TestPreflightRemoveBloqueiaEnderecoAusente(t *testing.T) {
	p := preflightOp(t, clienteComState(stateComCount, "available"),
		pedidoOp(StateOpRemove, "azurerm_key_vault.inexistente", ""))

	if !p.Blocked() {
		t.Fatal("deveria bloquear: o endereço não está no state")
	}
	if statusOp(p, "endereço existe") != CheckFail {
		t.Errorf("endereço existe = %q, quero fail", statusOp(p, "endereço existe"))
	}
	if len(p.Affected) != 0 {
		t.Errorf("Affected = %+v, quero vazio", p.Affected)
	}
}

// A verificação mais importante do rm: endereço sem índice leva junto todas as
// instâncias de um for_each.
func TestPreflightRemoveAvisaMultiplasInstancias(t *testing.T) {
	p := preflightOp(t, clienteComState(stateComCount, "available"),
		pedidoOp(StateOpRemove, "azurerm_subnet.sub", ""))

	if p.Blocked() {
		t.Fatalf("não deveria bloquear, só avisar: %+v", p.Checks)
	}
	if statusOp(p, "endereço existe") != CheckWarn {
		t.Errorf("endereço existe = %q, quero warn", statusOp(p, "endereço existe"))
	}
	if len(p.Affected) != 2 {
		t.Fatalf("Affected = %d, quero 2: %+v", len(p.Affected), p.Affected)
	}
	for _, r := range p.Affected {
		if !strings.HasPrefix(r.Address, "azurerm_subnet.sub[") {
			t.Errorf("instância inesperada: %q", r.Address)
		}
	}
}

// Com índice, só aquela instância entra.
func TestPreflightRemoveComIndiceAtingeUmaInstancia(t *testing.T) {
	p := preflightOp(t, clienteComState(stateComCount, "available"),
		pedidoOp(StateOpRemove, `azurerm_subnet.sub["app"]`, ""))

	if len(p.Affected) != 1 || p.Affected[0].Address != `azurerm_subnet.sub["app"]` {
		t.Errorf("Affected = %+v, quero só a instância app", p.Affected)
	}
	if statusOp(p, "endereço existe") != CheckOK {
		t.Errorf("endereço existe = %q, quero ok", statusOp(p, "endereço existe"))
	}
}

// Prefixo parecido não pode ser confundido com instância: "sub" não atinge
// "subrede".
func TestPreflightRemoveNaoConfundePrefixo(t *testing.T) {
	state := `{"version":4,"serial":1,"resources":[
      {"mode":"managed","type":"azurerm_subnet","name":"subrede","provider":"p","instances":[{}]}]}`

	p := preflightOp(t, clienteComState(state, "available"),
		pedidoOp(StateOpRemove, "azurerm_subnet.sub", ""))

	if !p.Blocked() {
		t.Errorf("azurerm_subnet.sub não deveria casar com subrede: %+v", p.Affected)
	}
}

func TestPreflightMoveCaminhoFeliz(t *testing.T) {
	p := preflightOp(t, clienteComState(stateComCount, "available"),
		pedidoOp(StateOpMove, "azurerm_storage_account.stg", "module.app.azurerm_storage_account.stg"))

	if p.Blocked() {
		t.Fatalf("bloqueado sem motivo: %+v", p.Checks)
	}
	if statusOp(p, "destino livre") != CheckOK {
		t.Errorf("destino livre = %q", statusOp(p, "destino livre"))
	}
}

// Mover para um endereço ocupado substituiria o que está lá.
func TestPreflightMoveBloqueiaDestinoOcupado(t *testing.T) {
	p := preflightOp(t, clienteComState(stateComCount, "available"),
		pedidoOp(StateOpMove, `azurerm_subnet.sub["app"]`, "azurerm_storage_account.stg"))

	if !p.Blocked() {
		t.Fatal("deveria bloquear: o destino já está no state")
	}
	if statusOp(p, "destino livre") != CheckFail {
		t.Errorf("destino livre = %q, quero fail", statusOp(p, "destino livre"))
	}
}

func TestPreflightStateOpBloqueiaStateTravado(t *testing.T) {
	client := &fakeStateClient{
		blobs: []azure.Blob{{
			Name: "dev/app.tfstate", SizeBytes: 4096, LeaseState: "leased",
			Metadata: lockMetadata(t, lockBody),
		}},
		conteudo: stateComCount,
	}

	p := preflightOp(t, client, pedidoOp(StateOpRemove, "azurerm_storage_account.stg", ""))

	if !p.Blocked() {
		t.Fatal("deveria bloquear: o state está travado")
	}
	if statusOp(p, "state destravado") != CheckFail {
		t.Errorf("state destravado = %q, quero fail", statusOp(p, "state destravado"))
	}
}

func TestPreflightStateOpBloqueiaStateInexistente(t *testing.T) {
	client := &fakeStateClient{blobs: []azure.Blob{{Name: "dev/outro.tfstate"}}}

	p := preflightOp(t, client, pedidoOp(StateOpRemove, "azurerm_storage_account.stg", ""))

	if !p.Blocked() {
		t.Fatal("deveria bloquear: o state não existe")
	}
	if len(p.Checks) != 1 {
		t.Errorf("checagens = %d, quero parar na primeira falha", len(p.Checks))
	}
}

func TestPreflightStateOpRecusaEntradaInvalida(t *testing.T) {
	client := clienteComState(stateComCount, "available")

	casos := map[string]StateOpRequest{
		"operação desconhecida": pedidoOp("destroy", "a.b", ""),
		"endereço vazio":        pedidoOp(StateOpRemove, "", ""),
		"endereço sem ponto":    pedidoOp(StateOpRemove, "azurerm_subnet", ""),
		"mv sem destino":        pedidoOp(StateOpMove, "a.b", ""),
		"mv com destino igual":  pedidoOp(StateOpMove, "a.b", "a.b"),
		"mv com destino ruim":   pedidoOp(StateOpMove, "a.b", "semponto"),
	}

	for name, req := range casos {
		t.Run(name, func(t *testing.T) {
			if _, err := PreflightStateOp(context.Background(), client, req); err == nil {
				t.Error("erro = nil, quero recusa")
			}
		})
	}
}

func TestPreflightStateOpPropagaErro(t *testing.T) {
	falha := errors.New("403 sem permissão")

	_, err := PreflightStateOp(context.Background(), &fakeStateClient{listErr: falha},
		pedidoOp(StateOpRemove, "a.b", ""))
	if !errors.Is(err, falha) {
		t.Errorf("erro = %v, quero %v", err, falha)
	}
}

func TestStateOpCommands(t *testing.T) {
	rm := preflightOp(t, clienteComState(stateComCount, "available"),
		pedidoOp(StateOpRemove, "azurerm_storage_account.stg", ""))
	if len(rm.Commands) != 2 || rm.Commands[1] != `terraform state rm 'azurerm_storage_account.stg'` {
		t.Errorf("comandos do rm = %+v", rm.Commands)
	}

	mv := preflightOp(t, clienteComState(stateComCount, "available"),
		pedidoOp(StateOpMove, "azurerm_storage_account.stg", "module.app.azurerm_storage_account.stg"))
	want := `terraform state mv 'azurerm_storage_account.stg' 'module.app.azurerm_storage_account.stg'`
	if len(mv.Commands) != 2 || mv.Commands[1] != want {
		t.Errorf("comandos do mv = %+v", mv.Commands)
	}
}

func renderOp(t *testing.T, p *StateOpPreflight, apply bool) string {
	t.Helper()

	var buf bytes.Buffer
	if err := WriteStateOpPreflight(&buf, p, apply); err != nil {
		t.Fatalf("WriteStateOpPreflight: %v", err)
	}
	return buf.String()
}

func TestWriteStateOpPreflightRemove(t *testing.T) {
	out := renderOp(t, preflightOp(t, clienteComState(stateComCount, "available"),
		pedidoOp(StateOpRemove, "azurerm_subnet.sub", "")), false)

	for _, want := range []string{
		"remover do state",
		"https://stterraform.blob.core.windows.net/time1",
		"ambiente",
		"Sai do state (2)",
		`azurerm_subnet.sub["app"]`,
		`azurerm_subnet.sub["db"]`,
		// O efeito colateral precisa estar dito, não subentendido.
		"continua existindo",
		"Dry-run: nada foi executado.",
		"terraform state rm 'azurerm_subnet.sub'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q:\n%s", want, out)
		}
	}
}

func TestWriteStateOpPreflightMove(t *testing.T) {
	out := renderOp(t, preflightOp(t, clienteComState(stateComCount, "available"),
		pedidoOp(StateOpMove, "azurerm_storage_account.stg", "module.app.azurerm_storage_account.stg")), false)

	if !strings.Contains(out, "Muda de endereço (1)") {
		t.Errorf("faltou a lista do que muda:\n%s", out)
	}
	if !strings.Contains(out, "destino") {
		t.Errorf("faltou o destino no cabeçalho:\n%s", out)
	}
	// O aviso de infraestrutura preservada é do rm, não do mv.
	if strings.Contains(out, "continua existindo") {
		t.Errorf("o aviso do rm vazou para o mv:\n%s", out)
	}
}

func TestWriteStateOpPreflightBloqueadoNaoSugereComando(t *testing.T) {
	out := renderOp(t, preflightOp(t, clienteComState(stateComCount, "available"),
		pedidoOp(StateOpRemove, "azurerm_key_vault.inexistente", "")), false)

	if !strings.Contains(out, "bloqueado. Nada foi executado.") {
		t.Errorf("faltou dizer que bloqueou:\n%s", out)
	}
	if strings.Contains(out, "terraform state rm '") {
		t.Errorf("não pode sugerir o comando bloqueado:\n%s", out)
	}
}

func TestWriteStateOpPreflightPropagaErroDeEscrita(t *testing.T) {
	p := preflightOp(t, clienteComState(stateComCount, "available"),
		pedidoOp(StateOpRemove, "azurerm_storage_account.stg", ""))

	if err := WriteStateOpPreflight(falhaNoWrite{}, p, false); err == nil {
		t.Error("WriteStateOpPreflight não propagou o erro de escrita")
	}
}
