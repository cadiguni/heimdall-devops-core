package terraform

import (
	"bytes"
	"strings"
	"testing"
)

func configDoWebapp() []ModuleFile {
	return []ModuleFile{
		{Name: "main.tf", Content: `
resource "azurerm_resource_group" "rg" {
  count    = 1
  name     = "app-dev-rg"
  location = "eastus"
}

resource "azurerm_linux_web_app" "wa" {
  name = "app-dev-wa"
}

resource "azurerm_storage_account" "outro" {
  name = "stcompart"
}

resource "azurerm_storage_container" "cont" {
  name = "dados"
}
`},
	}
}

// statePullDoRemoto devolve um state com o lineage que as fixtures usam, para o
// caso em que o diretório está inicializado contra o state certo.
func statePullDoRemoto() string { return stateParaRemap }

func snapshotOK() *ModuleSnapshot {
	return &ModuleSnapshot{
		Dir:      "../modulo",
		Files:    configDoWebapp(),
		RawState: statePullDoRemoto(),
	}
}

func planoComModulo(t *testing.T, snap *ModuleSnapshot) *RemapPlan {
	t.Helper()

	req := pedidoRemap("")
	req.Module = snap
	return planoRemap(t, clienteComState(stateParaRemap, "available"), req)
}

func TestCheckModuleCaminhoFeliz(t *testing.T) {
	p := planoComModulo(t, snapshotOK())

	if p.Blocked() {
		t.Fatalf("bloqueado sem motivo: %+v", p.Checks)
	}
	for _, nome := range []string{"módulo usa este state", "recursos na configuração", "configuração sem a assinatura antiga"} {
		if statusRemap(p, nome) != CheckOK {
			t.Errorf("%s = %q, queria ok", nome, statusRemap(p, nome))
		}
	}
	if p.ModuleDir != "../modulo" {
		t.Errorf("ModuleDir = %q", p.ModuleDir)
	}
}

// Sem --chdir as três verificações não acontecem, e isso precisa aparecer como
// aviso — não como silêncio.
func TestCheckModuleAusenteAvisa(t *testing.T) {
	p := planoComModulo(t, nil)

	if statusRemap(p, "módulo inspecionado") != CheckWarn {
		t.Errorf("módulo inspecionado = %q, queria warn", statusRemap(p, "módulo inspecionado"))
	}
	if p.Blocked() {
		t.Error("a falta de --chdir não deveria bloquear o dry-run")
	}
	// E as outras três não podem aparecer dizendo ok sobre o que não foi visto.
	if statusRemap(p, "recursos na configuração") != "" {
		t.Error("verificou configuração sem ter diretório")
	}
}

// É a verificação que justifica o resto existir: o 'state rm' funciona com o
// recurso fora da configuração, o 'import' recusa. Sem este check, o remap
// tiraria o recurso do state e falharia no import logo depois.
func TestCheckModuleRecursoSemConfiguracaoBloqueia(t *testing.T) {
	snap := snapshotOK()
	snap.Files = []ModuleFile{{Name: "main.tf", Content: `
resource "azurerm_resource_group" "rg" {
  count = 1
}
`}}

	p := planoComModulo(t, snap)

	if !p.Blocked() {
		t.Fatal("alvo sem bloco na configuração não bloqueou")
	}
	if statusRemap(p, "recursos na configuração") != CheckFail {
		t.Errorf("recursos na configuração = %q", statusRemap(p, "recursos na configuração"))
	}

	var detalhe string
	for _, c := range p.Checks {
		if c.Name == "recursos na configuração" {
			detalhe = c.Detail
		}
	}
	if !strings.Contains(detalhe, "azurerm_linux_web_app.wa") {
		t.Errorf("o detalhe precisa nomear o que falta: %q", detalhe)
	}
	// E precisa dizer por que isso importa, senão parece burocracia.
	if !strings.Contains(detalhe, "state rm") {
		t.Errorf("o detalhe não explica o risco: %q", detalhe)
	}
}

func TestCheckModuleDiretorioSemTfBloqueia(t *testing.T) {
	snap := snapshotOK()
	snap.Files = nil

	p := planoComModulo(t, snap)

	if !p.Blocked() {
		t.Fatal("diretório sem .tf não bloqueou")
	}
	if statusRemap(p, "recursos na configuração") != CheckFail {
		t.Errorf("recursos na configuração = %q", statusRemap(p, "recursos na configuração"))
	}
}

// Configuração em .tf.json existe e este código não interpreta. Reprovar seria
// afirmar o que não se sabe, então avisa.
func TestCheckModuleComTfJSONAvisaEmVezDeReprovar(t *testing.T) {
	snap := snapshotOK()
	snap.Files = []ModuleFile{{Name: "main.tf", Content: `resource "azurerm_resource_group" "rg" {}`}}
	snap.JSONFileCount = 2

	p := planoComModulo(t, snap)

	if statusRemap(p, "recursos na configuração") != CheckWarn {
		t.Errorf("recursos na configuração = %q, queria warn", statusRemap(p, "recursos na configuração"))
	}
	if p.Blocked() {
		t.Error("bloqueou sem poder afirmar que falta recurso")
	}
}

// Bloco comentado não conta como declaração: contar seria deixar passar
// exatamente o caso que o check existe para pegar.
func TestDeclaredResourcesIgnoraComentario(t *testing.T) {
	files := []ModuleFile{{Name: "main.tf", Content: `
# resource "azurerm_linux_web_app" "wa" {
//  resource "azurerm_service_plan" "sp" {
resource "azurerm_resource_group" "rg" {}
`}}

	declarados := declaredResources(files)
	if !declarados["azurerm_resource_group.rg"] {
		t.Error("não achou o bloco de verdade")
	}
	if declarados["azurerm_linux_web_app.wa"] || declarados["azurerm_service_plan.sp"] {
		t.Errorf("contou bloco comentado: %v", declarados)
	}
}

func TestConfigAddress(t *testing.T) {
	casos := []struct {
		address  string
		nome     string
		emModulo bool
	}{
		{"azurerm_resource_group.rg", "azurerm_resource_group.rg", false},
		{"azurerm_resource_group.rg[0]", "azurerm_resource_group.rg", false},
		{`azurerm_subnet.sub["app"]`, "azurerm_subnet.sub", false},
		{"module.app.azurerm_linux_web_app.wa", "azurerm_linux_web_app.wa", true},
		{"module.app.module.interno.azurerm_service_plan.sp[1]", "azurerm_service_plan.sp", true},
	}

	for _, c := range casos {
		nome, emModulo := configAddress(c.address)
		if nome != c.nome || emModulo != c.emModulo {
			t.Errorf("configAddress(%q) = (%q, %v), queria (%q, %v)",
				c.address, nome, emModulo, c.nome, c.emModulo)
		}
	}
}

// Recurso dentro de módulo não é verificável da raiz: o bloco mora no diretório
// do módulo, que pode vir de registry ou de git. Avisa, não reprova.
func TestCheckModuleRecursoEmModuloAvisa(t *testing.T) {
	const stateComModulo = `{
  "version": 4, "terraform_version": "1.9.8", "serial": 1, "lineage": "abc",
  "outputs": {},
  "resources": [
    {"module":"module.app","mode":"managed","type":"azurerm_resource_group","name":"rg","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{"attributes":{"id":"/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/app-dev-rg"}}]}
  ]
}`

	req := pedidoRemap("")
	req.Module = &ModuleSnapshot{
		Dir:      "../modulo",
		Files:    []ModuleFile{{Name: "main.tf", Content: `module "app" { source = "./app" }`}},
		RawState: stateComModulo,
	}

	p := planoRemap(t, clienteComState(stateComModulo, "available"), req)

	if statusRemap(p, "recursos na configuração") != CheckWarn {
		t.Errorf("recursos na configuração = %q, queria warn", statusRemap(p, "recursos na configuração"))
	}
	if p.Blocked() {
		t.Errorf("bloqueou recurso de módulo, que não é verificável daqui: %+v", p.Checks)
	}
}

// Lineage é a identidade do state. Diretório inicializado contra outro backend
// se denuncia aqui, e isso tem que reprovar: é o caso em que o rm acerta o
// state errado.
func TestCheckModuleStateDeOutroBackendBloqueia(t *testing.T) {
	snap := snapshotOK()
	snap.RawState = strings.Replace(stateParaRemap, `"lineage": "abc"`, `"lineage": "outro-state"`, 1)

	p := planoComModulo(t, snap)

	if !p.Blocked() {
		t.Fatal("diretório apontado para outro state não bloqueou")
	}
	if statusRemap(p, "módulo usa este state") != CheckFail {
		t.Errorf("módulo usa este state = %q", statusRemap(p, "módulo usa este state"))
	}
}

func TestCheckModuleStatePullQuebradoBloqueia(t *testing.T) {
	snap := snapshotOK()
	snap.RawState = ""
	snap.StatePullErr = "Backend initialization required"

	p := planoComModulo(t, snap)

	if !p.Blocked() {
		t.Fatal("falha no state pull não bloqueou")
	}

	var detalhe string
	for _, c := range p.Checks {
		if c.Name == "módulo usa este state" {
			detalhe = c.Detail
		}
	}
	if !strings.Contains(detalhe, "Backend initialization required") {
		t.Errorf("o detalhe perdeu a causa real: %q", detalhe)
	}
	// O heimdall não roda init, e precisa dizer isso em vez de deixar a pessoa
	// achar que o comando deveria resolver.
	if !strings.Contains(detalhe, "terraform init") {
		t.Errorf("o detalhe não lembra que o init é manual: %q", detalhe)
	}
}

// Remapear o state e deixar a configuração citando a assinatura velha é trocar
// o problema de lugar: o próximo plan volta para a antiga.
func TestCheckModuleConfiguracaoComAssinaturaAntigaAvisa(t *testing.T) {
	snap := snapshotOK()
	snap.Files = append(snap.Files, ModuleFile{
		Name:    "provider.tf",
		Content: `provider "azurerm" { subscription_id = "11111111-1111-1111-1111-111111111111" }`,
	})

	p := planoComModulo(t, snap)

	if statusRemap(p, "configuração sem a assinatura antiga") != CheckWarn {
		t.Errorf("= %q, queria warn", statusRemap(p, "configuração sem a assinatura antiga"))
	}
	// É aviso, não reprovação: a assinatura costuma vir de variável ou de
	// Variable Group, e a citação pode ser legítima.
	if p.Blocked() {
		t.Errorf("bloqueou por citação na configuração: %+v", p.Checks)
	}

	var detalhe string
	for _, c := range p.Checks {
		if c.Name == "configuração sem a assinatura antiga" {
			detalhe = c.Detail
		}
	}
	if !strings.Contains(detalhe, "provider.tf") {
		t.Errorf("o detalhe precisa dizer qual arquivo: %q", detalhe)
	}
}

func TestFilesMentioningIgnoraComentario(t *testing.T) {
	files := []ModuleFile{
		{Name: "a.tf", Content: `# era 11111111-1111-1111-1111-111111111111 antes`},
		{Name: "b.tf", Content: `subscription_id = "11111111-1111-1111-1111-111111111111"`},
	}

	achados := filesMentioning(files, assinaturaOrigem)
	if len(achados) != 1 || achados[0] != "b.tf" {
		t.Errorf("achados = %v", achados)
	}
}

// O state cru traz todos os atributos, segredo incluído. Ele entra no snapshot
// para ser parseado e não pode sair na renderização.
func TestWriteRemapPlanNaoImprimeStateCru(t *testing.T) {
	snap := snapshotOK()
	snap.RawState = strings.Replace(stateParaRemap,
		`"outputs": {}`, `"outputs": {"senha": {"value": "SEGREDO-DO-STATE-LOCAL"}}`, 1)

	p := planoComModulo(t, snap)

	var buf bytes.Buffer
	if err := WriteRemapPlan(&buf, p, false); err != nil {
		t.Fatalf("WriteRemapPlan: %v", err)
	}
	if strings.Contains(buf.String(), "SEGREDO-DO-STATE-LOCAL") {
		t.Errorf("o state do diretório vazou na saída:\n%s", buf.String())
	}
}

// Em diretório sem backend inicializado o 'terraform state pull' sai com código
// 0 e não imprime nada. O vazio precisa virar uma mensagem que aponta a causa,
// não "state inválido: EOF".
func TestCheckModuleStateVazioDizQueFaltaInit(t *testing.T) {
	snap := snapshotOK()
	snap.RawState = ""

	p := planoComModulo(t, snap)

	if !p.Blocked() {
		t.Fatal("state pull vazio não bloqueou")
	}

	var detalhe string
	for _, c := range p.Checks {
		if c.Name == "módulo usa este state" {
			detalhe = c.Detail
		}
	}
	if strings.Contains(detalhe, "EOF") {
		t.Errorf("a mensagem expõe o erro do parser em vez da causa: %q", detalhe)
	}
	if !strings.Contains(detalhe, "terraform init") {
		t.Errorf("a mensagem não aponta o init: %q", detalhe)
	}
}
