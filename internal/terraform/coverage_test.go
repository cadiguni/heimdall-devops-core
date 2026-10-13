package terraform

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

const subCoverage = "92dbecc2-c36d-4af2-887d-3681969e5850"
const subOutra = "51852c54-7b67-46c5-a756-c53b4bcf0504"

func idDe(sub, grupo, resto string) string {
	return "/subscriptions/" + sub + "/resourceGroups/" + grupo + resto
}

func gruposDaAssinatura() []azure.ResourceGroup {
	return []azure.ResourceGroup{
		{Name: "atena-dev-rg", Location: "eastus", Tags: map[string]string{"Setor": "NAP"}},
		{Name: "rg-gvCert", Location: "eastus"},
		{Name: "terraform", Location: "eastus", Tags: map[string]string{"Setor": "NAP"}},
	}
}

func trackingDeTeste() []StateTracking {
	return []StateTracking{
		TrackingOf("dev/atena-dev.tfstate", []StateResource{
			{Address: "azurerm_resource_group.rg", Mode: "managed", ID: idDe(subCoverage, "atena-dev-rg", "")},
			{Address: "azurerm_linux_web_app.wa", Mode: "managed",
				ID: idDe(subCoverage, "atena-dev-rg", "/providers/Microsoft.Web/sites/atena")},
		}),
		TrackingOf("dev/outro.tfstate", []StateResource{
			{Address: "azurerm_resource_group.rg", Mode: "managed", ID: idDe(subOutra, "fora-rg", "")},
		}),
	}
}

func TestBuildCoverageSeparaComSemEAusente(t *testing.T) {
	states := append(trackingDeTeste(),
		TrackingOf("dev/sumido.tfstate", []StateResource{
			{Address: "azurerm_resource_group.rg", Mode: "managed", ID: idDe(subCoverage, "ja-foi-rg", "")},
		}))

	r := BuildCoverage(subCoverage, gruposDaAssinatura(), states)

	comState := r.ByStatus(CoverageTracked)
	if len(comState) != 1 || comState[0].ResourceGroup != "atena-dev-rg" {
		t.Fatalf("com state = %+v", comState)
	}
	if len(comState[0].States) != 1 || comState[0].States[0] != "dev/atena-dev.tfstate" {
		t.Errorf("states do grupo = %v", comState[0].States)
	}

	semState := r.ByStatus(CoverageUntracked)
	if len(semState) != 2 {
		t.Errorf("sem state = %+v", semState)
	}

	ausente := r.ByStatus(CoverageMissing)
	if len(ausente) != 1 || ausente[0].ResourceGroup != "ja-foi-rg" {
		t.Errorf("ausente = %+v", ausente)
	}
}

// State de outra assinatura não pode contar como cobertura desta: contaria um
// resource group como gerenciado por um state que não fala dele.
func TestBuildCoverageIgnoraOutraAssinatura(t *testing.T) {
	r := BuildCoverage(subCoverage, gruposDaAssinatura(), trackingDeTeste())

	for _, e := range r.Entries {
		if e.ResourceGroup == "fora-rg" {
			t.Errorf("grupo de outra assinatura entrou no relatório: %+v", e)
		}
	}
}

// Data source não rastreia nada: é relido a cada plan. Contá-lo diria que um
// resource group tem state quando não tem.
func TestTrackingOfIgnoraDataSource(t *testing.T) {
	tr := TrackingOf("x.tfstate", []StateResource{
		{Address: "data.azurerm_resource_group.rg", Mode: "data", ID: idDe(subCoverage, "so-lido-rg", "")},
		{Address: "azurerm_resource_group.rg", Mode: "managed", ID: idDe(subCoverage, "gerenciado-rg", "")},
	})

	grupos := tr.Groups[subCoverage]
	if len(grupos) != 1 || grupos[0] != "gerenciado-rg" {
		t.Errorf("grupos = %v", grupos)
	}
}

func TestTrackingOfIgnoraIDSemResourceGroup(t *testing.T) {
	tr := TrackingOf("x.tfstate", []StateResource{
		{Address: "azurerm_storage_container.c", Mode: "managed",
			ID: "https://stcompart.blob.core.windows.net/dados"},
		{Address: "azurerm_subscription.s", Mode: "managed", ID: "/subscriptions/" + subCoverage},
		{Address: "nulo.x", Mode: "managed", ID: ""},
	})

	if len(tr.Groups) != 0 {
		t.Errorf("grupos = %v, queria nenhum", tr.Groups)
	}
}

// Dois states no mesmo resource group é informação, não erro: costuma ser
// divisão deliberada, às vezes é duplicata. Os dois precisam aparecer.
func TestBuildCoverageMostraTodosOsStatesDoGrupo(t *testing.T) {
	states := []StateTracking{
		TrackingOf("dev/a.tfstate", []StateResource{
			{Mode: "managed", ID: idDe(subCoverage, "atena-dev-rg", "")},
		}),
		TrackingOf("dev/b.tfstate", []StateResource{
			{Mode: "managed", ID: idDe(subCoverage, "atena-dev-rg", "/providers/Microsoft.Web/sites/x")},
		}),
	}

	r := BuildCoverage(subCoverage, gruposDaAssinatura(), states)

	comState := r.ByStatus(CoverageTracked)
	if len(comState) != 1 || len(comState[0].States) != 2 {
		t.Fatalf("com state = %+v", comState)
	}
	if comState[0].States[0] != "dev/a.tfstate" || comState[0].States[1] != "dev/b.tfstate" {
		t.Errorf("ordem instável: %v", comState[0].States)
	}
}

// O nome do resource group vem de fonte diferente em cada lado (ARM e id do
// state) e a caixa não bate sempre.
func TestBuildCoverageIgnoraCaixaNoNome(t *testing.T) {
	states := []StateTracking{
		TrackingOf("dev/a.tfstate", []StateResource{
			{Mode: "managed", ID: idDe(subCoverage, "ATENA-DEV-RG", "")},
		}),
	}

	r := BuildCoverage(strings.ToUpper(subCoverage), gruposDaAssinatura(), states)

	if len(r.ByStatus(CoverageTracked)) != 1 {
		t.Errorf("a diferença de caixa criou um falso 'sem state': %+v", r.Entries)
	}
}

// State ilegível não é state vazio. Tratar como vazio diria que os resource
// groups dele não têm state, que é a conclusão errada e a mais cara.
func TestBuildCoverageStateIlegivelViraNota(t *testing.T) {
	states := append(trackingDeTeste(), StateTracking{Path: "dev/quebrado.tfstate", Err: "state inválido: EOF"})

	r := BuildCoverage(subCoverage, gruposDaAssinatura(), states)

	if len(r.Notes) != 1 || !strings.Contains(r.Notes[0], "dev/quebrado.tfstate") {
		t.Fatalf("notas = %v", r.Notes)
	}
	if !strings.Contains(r.Notes[0], "EOF") {
		t.Errorf("a nota perdeu a causa: %q", r.Notes[0])
	}
}

// --- renderização ---

func TestWriteCoverageMostraAsTresSecoes(t *testing.T) {
	states := append(trackingDeTeste(),
		TrackingOf("dev/sumido.tfstate", []StateResource{
			{Mode: "managed", ID: idDe(subCoverage, "ja-foi-rg", "")},
		}))
	r := BuildCoverage(subCoverage, gruposDaAssinatura(), states)
	r.Container = "https://st.blob.core.windows.net/nap"

	var buf bytes.Buffer
	if err := WriteCoverage(&buf, r, false); err != nil {
		t.Fatalf("WriteCoverage: %v", err)
	}
	saida := buf.String()

	for _, querido := range []string{
		subCoverage, "nap",
		"Com state (1)", "atena-dev.tfstate",
		"Sem state neste container (2)", "rg-gvCert",
		"State rastreia resource group que não está na assinatura (1)", "ja-foi-rg",
		"states remap",
	} {
		if !strings.Contains(saida, querido) {
			t.Errorf("saída sem %q:\n%s", querido, saida)
		}
	}

	// A ressalva não é opcional: sem ela "sem state" lê como "feito à mão".
	if !strings.Contains(saida, "outro container") {
		t.Errorf("sem a ressalva sobre outro backend:\n%s", saida)
	}
}

// Sem state, a tag é o que resta para dizer de quem é o recurso.
func TestWriteCoverageTagsSoComAFlag(t *testing.T) {
	r := BuildCoverage(subCoverage, gruposDaAssinatura(), trackingDeTeste())

	var semFlag, comFlag bytes.Buffer
	if err := WriteCoverage(&semFlag, r, false); err != nil {
		t.Fatalf("WriteCoverage: %v", err)
	}
	if err := WriteCoverage(&comFlag, r, true); err != nil {
		t.Fatalf("WriteCoverage: %v", err)
	}

	if strings.Contains(semFlag.String(), "Setor=NAP") {
		t.Errorf("tags apareceram sem a flag:\n%s", semFlag.String())
	}
	if !strings.Contains(comFlag.String(), "Setor=NAP") {
		t.Errorf("tags não apareceram com a flag:\n%s", comFlag.String())
	}
	if !strings.Contains(comFlag.String(), "sem tags") {
		t.Errorf("grupo sem tag precisa dizer isso:\n%s", comFlag.String())
	}
}

// Container vazio faria todo resource group aparecer como sem state. Sem o
// aviso, isso pareceria um diagnóstico.
func TestWriteCoverageContainerVazioAvisa(t *testing.T) {
	r := BuildCoverage(subCoverage, gruposDaAssinatura(), nil)

	var buf bytes.Buffer
	if err := WriteCoverage(&buf, r, false); err != nil {
		t.Fatalf("WriteCoverage: %v", err)
	}
	if !strings.Contains(buf.String(), "Nenhum state no container") {
		t.Errorf("container vazio não se denunciou:\n%s", buf.String())
	}
}

func TestWriteCoverageAssinaturaVaziaAvisa(t *testing.T) {
	r := BuildCoverage(subCoverage, nil, trackingDeTeste())

	var buf bytes.Buffer
	if err := WriteCoverage(&buf, r, false); err != nil {
		t.Fatalf("WriteCoverage: %v", err)
	}
	if !strings.Contains(buf.String(), "assinatura errada") {
		t.Errorf("assinatura vazia não se denunciou:\n%s", buf.String())
	}
}
