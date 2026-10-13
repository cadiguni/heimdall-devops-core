package terraform

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

const assinaturaOrigem = "11111111-1111-1111-1111-111111111111"
const assinaturaDestino = "22222222-2222-2222-2222-222222222222"

// stateParaRemap tem um de cada categoria: um recurso reescrevível, outro de
// outro resource group, um que não menciona assinatura nenhuma, e um data
// source.
const stateParaRemap = `{
  "version": 4, "terraform_version": "1.9.8", "serial": 7, "lineage": "abc",
  "outputs": {},
  "resources": [
    {"mode":"managed","type":"azurerm_resource_group","name":"rg","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{"attributes":{"id":"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/app-dev-rg"}}]},
    {"mode":"managed","type":"azurerm_linux_web_app","name":"wa","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{"attributes":{"id":"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/app-dev-rg/providers/Microsoft.Web/sites/app-dev-wa"}}]},
    {"mode":"managed","type":"azurerm_storage_account","name":"outro","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{"attributes":{"id":"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/compartilhado-rg/providers/Microsoft.Storage/storageAccounts/stcompart"}}]},
    {"mode":"managed","type":"azurerm_storage_container","name":"cont","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{"attributes":{"id":"https://stcompart.blob.core.windows.net/dados"}}]},
    {"mode":"data","type":"azurerm_client_config","name":"atual","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{"attributes":{"id":"/subscriptions/11111111-1111-1111-1111-111111111111"}}]}
  ]
}`

func pedidoRemap(rg string) RemapRequest {
	return RemapRequest{
		From:          assinaturaOrigem,
		To:            assinaturaDestino,
		ResourceGroup: rg,
		Account:       "stterraform",
		Container:     "time1",
		Key:           "dev/app.tfstate",
		ContainerURL:  "https://stterraform.blob.core.windows.net/time1",
	}
}

func planoRemap(t *testing.T, client StateClient, req RemapRequest) *RemapPlan {
	t.Helper()

	p, err := PlanRemap(context.Background(), client, req)
	if err != nil {
		t.Fatalf("PlanRemap: %v", err)
	}
	return p
}

func statusRemap(p *RemapPlan, name string) CheckStatus {
	for _, c := range p.Checks {
		if c.Name == name {
			return c.Status
		}
	}
	return ""
}

func categoriaDe(p *RemapPlan, address string) RemapEntry {
	for _, e := range p.Entries {
		if e.Address == address {
			return e
		}
	}
	return RemapEntry{}
}

func TestPlanRemapSemFiltroPegaTudoDaAssinatura(t *testing.T) {
	p := planoRemap(t, clienteComState(stateParaRemap, "available"), pedidoRemap(""))

	if p.Blocked() {
		t.Fatalf("bloqueado sem motivo: %+v", p.Checks)
	}

	alvos := p.Targets()
	if len(alvos) != 3 {
		t.Fatalf("alvos = %d, queria 3: %+v", len(alvos), alvos)
	}

	rg := categoriaDe(p, "azurerm_resource_group.rg")
	queria := "/subscriptions/" + assinaturaDestino + "/resourceGroups/app-dev-rg"
	if rg.NewID != queria {
		t.Errorf("NewID = %q, queria %q", rg.NewID, queria)
	}
	if rg.CurrentID == rg.NewID {
		t.Error("CurrentID e NewID iguais: nada foi reescrito")
	}
}

// O --rg é o que impede a troca de atingir recurso da mesma assinatura que não
// foi movido — apontar o state para um id que não existe é pior que não mexer.
func TestPlanRemapFiltraPorResourceGroup(t *testing.T) {
	p := planoRemap(t, clienteComState(stateParaRemap, "available"), pedidoRemap("app-dev-rg"))

	alvos := p.Targets()
	if len(alvos) != 2 {
		t.Fatalf("alvos = %d, queria 2: %+v", len(alvos), alvos)
	}

	outro := categoriaDe(p, "azurerm_storage_account.outro")
	if outro.Category != RemapUntouched {
		t.Errorf("recurso de outro RG = %q, queria untouched", outro.Category)
	}
	if !strings.Contains(outro.Reason, "app-dev-rg") {
		t.Errorf("Reason não cita o RG filtrado: %q", outro.Reason)
	}
	if outro.NewID != "" {
		t.Errorf("recurso fora do filtro ganhou NewID: %q", outro.NewID)
	}
}

func TestPlanRemapIgnoraDataSourceEIDSemAssinatura(t *testing.T) {
	p := planoRemap(t, clienteComState(stateParaRemap, "available"), pedidoRemap(""))

	ds := categoriaDe(p, "data.azurerm_client_config.atual")
	if ds.Category != RemapUntouched {
		t.Errorf("data source = %q, queria untouched", ds.Category)
	}
	if ds.NewID != "" {
		t.Errorf("data source ganhou NewID: %q", ds.NewID)
	}

	cont := categoriaDe(p, "azurerm_storage_container.cont")
	if cont.Category != RemapUntouched {
		t.Errorf("container (id é URL) = %q, queria untouched", cont.Category)
	}
}

// A terceira categoria existe por causa deste caso: a assinatura está no id,
// mas não como segmento de caminho. Reescrever às cegas daria um id inválido, e
// ignorar deixaria a migração pela metade sem ninguém notar — então bloqueia.
func TestPlanRemapBloqueiaIDOpaco(t *testing.T) {
	const comBase64 = `{
  "version": 4, "terraform_version": "1.9.8", "serial": 1, "lineage": "abc",
  "outputs": {},
  "resources": [
    {"mode":"managed","type":"azurerm_resource_group","name":"rg","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{"attributes":{"id":"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/app-dev-rg"}}]},
    {"mode":"managed","type":"azurerm_monitor_diagnostic_setting","name":"diag","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{"attributes":{"id":"sub=11111111-1111-1111-1111-111111111111|diag"}}]}
  ]
}`

	p := planoRemap(t, clienteComState(comBase64, "available"), pedidoRemap(""))

	opacos := p.Opaque()
	if len(opacos) != 1 || opacos[0].Address != "azurerm_monitor_diagnostic_setting.diag" {
		t.Fatalf("opacos = %+v", opacos)
	}
	if !p.Blocked() {
		t.Error("id não reescrevível não bloqueou o plano")
	}
	if statusRemap(p, "ids não reescrevíveis") != CheckFail {
		t.Errorf("ids não reescrevíveis = %q", statusRemap(p, "ids não reescrevíveis"))
	}
	// O alvo legítimo continua listado: quem vai corrigir à mão precisa ver o
	// plano inteiro, não só o que travou.
	if len(p.Targets()) != 1 {
		t.Errorf("alvos = %+v", p.Targets())
	}
}

func TestPlanRemapSemAlvoBloqueia(t *testing.T) {
	req := pedidoRemap("")
	req.From = "99999999-9999-9999-9999-999999999999"

	p := planoRemap(t, clienteComState(stateParaRemap, "available"), req)

	if !p.Blocked() {
		t.Error("state sem nenhum id da assinatura de origem não bloqueou")
	}
	if statusRemap(p, "recursos a remapear") != CheckFail {
		t.Errorf("recursos a remapear = %q", statusRemap(p, "recursos a remapear"))
	}
}

func TestPlanRemapStateTravadoBloqueia(t *testing.T) {
	p := planoRemap(t, clienteComState(stateParaRemap, "leased"), pedidoRemap(""))

	if !p.Blocked() {
		t.Error("state com lease não bloqueou")
	}
	if statusRemap(p, "state destravado") != CheckFail {
		t.Errorf("state destravado = %q", statusRemap(p, "state destravado"))
	}
}

func TestPlanRemapStateInexistenteBloqueia(t *testing.T) {
	client := &fakeStateClient{blobs: []azure.Blob{}}

	p := planoRemap(t, client, pedidoRemap(""))

	if !p.Blocked() {
		t.Error("state inexistente não bloqueou")
	}
	if statusRemap(p, "state existe") != CheckFail {
		t.Errorf("state existe = %q", statusRemap(p, "state existe"))
	}
}

// O backend na assinatura de origem é a armadilha mais séria do fluxo, e o
// heimdall não consegue verificar: saber em que assinatura o storage account
// mora é control plane, e quem roda pode ter só acesso de dados. Então avisa
// sempre — e isso é aviso, não reprovação, senão o comando nunca passaria.
func TestPlanRemapAvisaSobreBackend(t *testing.T) {
	p := planoRemap(t, clienteComState(stateParaRemap, "available"), pedidoRemap(""))

	if statusRemap(p, "backend fora do escopo") != CheckWarn {
		t.Errorf("backend fora do escopo = %q, queria warn", statusRemap(p, "backend fora do escopo"))
	}

	var detalhe string
	for _, c := range p.Checks {
		if c.Name == "backend fora do escopo" {
			detalhe = c.Detail
		}
	}
	if !strings.Contains(detalhe, "stterraform") || !strings.Contains(detalhe, assinaturaOrigem) {
		t.Errorf("o aviso precisa dizer qual conta e qual assinatura: %q", detalhe)
	}
}

func TestPlanRemapRecusaOrigemIgualDestino(t *testing.T) {
	req := pedidoRemap("")
	req.To = strings.ToUpper(req.From)

	if _, err := PlanRemap(context.Background(), clienteComState(stateParaRemap, "available"), req); err == nil {
		t.Error("origem e destino iguais (variando caixa) deveriam ser recusados")
	}
}

func TestPlanRemapExigeAsDuasAssinaturas(t *testing.T) {
	req := pedidoRemap("")
	req.To = ""

	if _, err := PlanRemap(context.Background(), clienteComState(stateParaRemap, "available"), req); err == nil {
		t.Error("--to vazio deveria ser recusado")
	}
}

// GUID em id do Azure aparece em caixas diferentes dependendo de quem escreveu.
func TestClassifyForRemapIgnoraCaixa(t *testing.T) {
	recursos := []StateResource{{
		Address: "azurerm_resource_group.rg",
		Mode:    "managed",
		ID:      "/SUBSCRIPTIONS/11111111-1111-1111-1111-111111111111/resourceGroups/app-dev-rg",
	}}

	entries := classifyForRemap(recursos, pedidoRemap(""))
	if entries[0].Category != RemapTarget {
		t.Fatalf("categoria = %q, queria remap", entries[0].Category)
	}
	if !strings.Contains(entries[0].NewID, assinaturaDestino) {
		t.Errorf("NewID = %q", entries[0].NewID)
	}
}

// --- execução ---

type fakeRunner struct {
	calls   []string
	failOn  string
	failRm  bool
	failErr error
}

func (f *fakeRunner) Import(_ context.Context, address, resourceID string) error {
	f.calls = append(f.calls, "import "+address+" "+resourceID)
	if address == f.failOn && !f.failRm {
		return f.failErr
	}
	return nil
}

func (f *fakeRunner) StateRm(_ context.Context, address string) error {
	f.calls = append(f.calls, "rm "+address)
	if address == f.failOn && f.failRm {
		return f.failErr
	}
	return nil
}

func (f *fakeRunner) StateMv(_ context.Context, source, destination string) error {
	f.calls = append(f.calls, "mv "+source+" "+destination)
	return nil
}

func alvosDeTeste() []RemapEntry {
	return []RemapEntry{
		{Address: "a", Category: RemapTarget, CurrentID: "/subscriptions/old/a", NewID: "/subscriptions/new/a"},
		{Address: "b", Category: RemapTarget, CurrentID: "/subscriptions/old/b", NewID: "/subscriptions/new/b"},
		{Address: "c", Category: RemapTarget, CurrentID: "/subscriptions/old/c", NewID: "/subscriptions/new/c"},
	}
}

func TestExecuteRemapRmAntesDoImport(t *testing.T) {
	runner := &fakeRunner{}

	r, err := ExecuteRemap(context.Background(), runner, alvosDeTeste(), nil)
	if err != nil {
		t.Fatalf("ExecuteRemap: %v", err)
	}
	if len(r.Done) != 3 {
		t.Errorf("Done = %v", r.Done)
	}

	// A ordem importa: o endereço precisa estar livre para o import entrar.
	queria := []string{
		"rm a", "import a /subscriptions/new/a",
		"rm b", "import b /subscriptions/new/b",
		"rm c", "import c /subscriptions/new/c",
	}
	if strings.Join(runner.calls, "|") != strings.Join(queria, "|") {
		t.Errorf("chamadas = %v", runner.calls)
	}
}

func TestExecuteRemapParaNaPrimeiraFalha(t *testing.T) {
	runner := &fakeRunner{failOn: "b", failErr: errors.New("boom")}

	r, err := ExecuteRemap(context.Background(), runner, alvosDeTeste(), nil)
	if err == nil {
		t.Fatal("falha no import não virou erro")
	}
	if r.FailedAt != "b" || r.Phase != "import" {
		t.Errorf("FailedAt=%q Phase=%q", r.FailedAt, r.Phase)
	}
	if len(r.Done) != 1 || r.Done[0] != "a" {
		t.Errorf("Done = %v", r.Done)
	}
	// Não encosta no c: parar significa parar.
	for _, c := range runner.calls {
		if strings.HasSuffix(c, " c") || strings.Contains(c, " c ") {
			t.Errorf("mexeu no alvo seguinte: %v", runner.calls)
		}
	}
}

// Falha no import deixa o recurso fora do state, existindo no Azure. É o estado
// intermediário perigoso, e a saída precisa dizer como sair dele.
func TestWriteRemapResultFalhaNoImportMostraRetomada(t *testing.T) {
	var buf bytes.Buffer
	r := &RemapResult{Done: []string{"a"}, FailedAt: "b", Phase: "import"}

	if err := WriteRemapResult(&buf, r, alvosDeTeste()); err != nil {
		t.Fatalf("WriteRemapResult: %v", err)
	}

	saida := buf.String()
	if !strings.Contains(saida, "terraform import 'b' '/subscriptions/new/b'") {
		t.Errorf("sem o comando de retomada com o id novo:\n%s", saida)
	}
	if !strings.Contains(saida, "saiu do state") {
		t.Errorf("sem dizer que o recurso está fora do state:\n%s", saida)
	}
	if !strings.Contains(saida, "c") {
		t.Errorf("sem listar o que falta:\n%s", saida)
	}
}

// Falha no rm não deixa buraco: o recurso continua no state com o id velho. Não
// faz sentido sugerir import aqui.
func TestWriteRemapResultFalhaNoRmNaoSugereImport(t *testing.T) {
	var buf bytes.Buffer
	r := &RemapResult{Done: []string{"a"}, FailedAt: "b", Phase: "rm"}

	if err := WriteRemapResult(&buf, r, alvosDeTeste()); err != nil {
		t.Fatalf("WriteRemapResult: %v", err)
	}

	if strings.Contains(buf.String(), "terraform import") {
		t.Errorf("sugeriu import para falha no rm:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "fase de rm") {
		t.Errorf("sem dizer a fase:\n%s", buf.String())
	}
}

func TestWriteRemapResultSucessoResume(t *testing.T) {
	var buf bytes.Buffer
	r := &RemapResult{Done: []string{"a", "b", "c"}}

	if err := WriteRemapResult(&buf, r, alvosDeTeste()); err != nil {
		t.Fatalf("WriteRemapResult: %v", err)
	}
	if !strings.Contains(buf.String(), "3 recurso(s) remapeados") {
		t.Errorf("saída = %q", buf.String())
	}
}

// --- renderização ---

func TestWriteRemapPlanMostraAlvoEOsTresGrupos(t *testing.T) {
	p := planoRemap(t, clienteComState(stateParaRemap, "available"), pedidoRemap("app-dev-rg"))

	var buf bytes.Buffer
	if err := WriteRemapPlan(&buf, p, false); err != nil {
		t.Fatalf("WriteRemapPlan: %v", err)
	}
	saida := buf.String()

	// Cabeçalho: contra o que está operando. Dev, hml e prod só se distinguem
	// pelo caminho.
	for _, querido := range []string{
		"dev/app.tfstate", "app-dev-rg", assinaturaOrigem, assinaturaDestino,
		"Serão remapeados", "Não serão tocados", "Dry-run: nada foi executado",
	} {
		if !strings.Contains(saida, querido) {
			t.Errorf("saída sem %q:\n%s", querido, saida)
		}
	}

	if !strings.Contains(saida, "ANTES") {
		t.Errorf("o dry-run precisa avisar que o move vem antes:\n%s", saida)
	}
}

// Plano bloqueado não imprime o comando equivalente: sugerir seria convidar a
// contornar a verificação.
func TestWriteRemapPlanBloqueadoNaoSugereComando(t *testing.T) {
	p := planoRemap(t, clienteComState(stateParaRemap, "leased"), pedidoRemap(""))

	var buf bytes.Buffer
	if err := WriteRemapPlan(&buf, p, false); err != nil {
		t.Fatalf("WriteRemapPlan: %v", err)
	}

	if !strings.Contains(buf.String(), "Remap bloqueado") {
		t.Errorf("saída = %q", buf.String())
	}
	if strings.Contains(buf.String(), "--apply") {
		t.Errorf("plano bloqueado sugeriu --apply:\n%s", buf.String())
	}
}

// O id é o único atributo lido do state, e nenhum valor de atributo pode
// aparecer na saída.
func TestWriteRemapPlanNaoImprimeOutroAtributo(t *testing.T) {
	const comSegredo = `{
  "version": 4, "terraform_version": "1.9.8", "serial": 1, "lineage": "abc",
  "outputs": {},
  "resources": [
    {"mode":"managed","type":"azurerm_storage_account","name":"stg","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{"attributes":{
       "id":"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/app-dev-rg/providers/Microsoft.Storage/storageAccounts/stg",
       "primary_access_key":"SENHA-QUE-NAO-PODE-SAIR",
       "primary_connection_string":"DefaultEndpointsProtocol=https;AccountKey=SENHA-QUE-NAO-PODE-SAIR"}}]}
  ]
}`

	p := planoRemap(t, clienteComState(comSegredo, "available"), pedidoRemap(""))

	var buf bytes.Buffer
	if err := WriteRemapPlan(&buf, p, false); err != nil {
		t.Fatalf("WriteRemapPlan: %v", err)
	}
	if strings.Contains(buf.String(), "SENHA-QUE-NAO-PODE-SAIR") {
		t.Errorf("valor de atributo vazou na saída:\n%s", buf.String())
	}
}
