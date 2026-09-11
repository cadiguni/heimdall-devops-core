package pipeline

import (
	"os"
	"strings"
	"testing"
)

func diagnoseFixture(t *testing.T) *Diagnosis {
	t.Helper()

	f, err := os.Open("testdata/pipeline_log_misto.txt")
	if err != nil {
		t.Fatalf("abrindo fixture: %v", err)
	}
	defer f.Close()

	d, err := Diagnose(f, "pipeline_log_misto.txt")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	return d
}

func countBySignature(d *Diagnosis) map[string]int {
	counts := map[string]int{}
	for _, f := range d.Findings {
		counts[f.Signature]++
	}
	return counts
}

// A fixture concatena 11 falhas reais; cada assinatura do catálogo precisa
// aparecer com a contagem esperada.
func TestDiagnoseFixtureCobreOsOnzeCasos(t *testing.T) {
	got := countBySignature(diagnoseFixture(t))

	want := map[string]int{
		"cdn-custom-domain-cname":      1,
		"undeclared-variable":          7, // 1 do caso 2 + 6 do caso 7
		"resource-already-exists":      2,
		"resource-group-not-found":     1,
		"missing-pipeline-input":       1,
		"invalid-storage-account-name": 1,
		"provider-schema-mismatch":     4,
		"parent-resource-not-found":    1,
		"azure-resource-not-found":     1,
		"deprecated-argument":          2,
	}

	for sig, n := range want {
		if got[sig] != n {
			t.Errorf("%s: %d ocorrências, quero %d", sig, got[sig], n)
		}
	}
	for sig, n := range got {
		if _, esperada := want[sig]; !esperada {
			t.Errorf("assinatura inesperada %s (%d ocorrências)", sig, n)
		}
	}
}

// Ruído que aparece em toda falha não pode virar achado.
func TestDiagnoseIgnoraErrosDeConsequencia(t *testing.T) {
	log := `##[error]Error: TerraformPlanFailed 1
##[error]Error: The process '/opt/terraform' failed with exit code 1
Releasing state lock. This may take a few moments...
Acquiring state lock. This may take a few moments...
##[warning]Can't find loc string for key: TerraformPlanFailed`

	d, err := Diagnose(strings.NewReader(log), "")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if d.HasFindings() {
		t.Errorf("achados em log só de consequência: %+v", d.Findings)
	}
}

// O caso mais valioso: extrair endereço e ID e montar o import.
func TestDiagnoseMontaComandoDeImport(t *testing.T) {
	log := `╷
│ Error: a resource with the ID "/subscriptions/abc/resourceGroups/meu-rg" already exists - to be managed via Terraform this resource needs to be imported into the State.
│
│   with azurerm_resource_group.rg[0],
│   on azurerm_resource_group.tf line 2, in resource "azurerm_resource_group" "rg":
│    2: resource "azurerm_resource_group" "rg" {
│
╵`

	d, err := Diagnose(strings.NewReader(log), "")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if len(d.Findings) != 1 {
		t.Fatalf("achados = %d, quero 1: %+v", len(d.Findings), d.Findings)
	}

	f := d.Findings[0]
	if f.Details["address"] != "azurerm_resource_group.rg[0]" {
		t.Errorf("address = %q", f.Details["address"])
	}
	if f.Details["resource_id"] != "/subscriptions/abc/resourceGroups/meu-rg" {
		t.Errorf("resource_id = %q", f.Details["resource_id"])
	}

	want := `terraform import 'azurerm_resource_group.rg[0]' '/subscriptions/abc/resourceGroups/meu-rg'`
	if len(f.Commands) != 1 || f.Commands[0] != want {
		t.Errorf("comandos = %v, quero [%s]", f.Commands, want)
	}
}

// Sem o bloco de contexto não dá para saber o endereço. Melhor um achado sem
// comando do que um import com endereço adivinhado.
func TestDiagnoseNaoInventaEnderecoParaImport(t *testing.T) {
	log := `│ Error: a resource with the ID "/subscriptions/abc/resourceGroups/meu-rg" already exists - to be managed via Terraform this resource needs to be imported into the State.`

	d, err := Diagnose(strings.NewReader(log), "")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if len(d.Findings) != 1 {
		t.Fatalf("achados = %d, quero 1", len(d.Findings))
	}
	if len(d.Findings[0].Commands) != 0 {
		t.Errorf("comandos = %v, quero nenhum sem endereço", d.Findings[0].Commands)
	}
	if _, tem := d.Findings[0].Details["address"]; tem {
		t.Error("address não deveria existir sem o bloco de contexto")
	}
}

func TestDiagnoseSeveridades(t *testing.T) {
	d := diagnoseFixture(t)

	erros := d.Errors()
	if len(erros) != len(d.Findings)-2 {
		t.Errorf("erros = %d de %d achados, quero todos menos os 2 avisos", len(erros), len(d.Findings))
	}
	for _, f := range erros {
		if f.Severity != SeverityError {
			t.Errorf("%s: severidade %q em Errors()", f.Signature, f.Severity)
		}
	}
}

func TestDiagnoseLogVazio(t *testing.T) {
	d, err := Diagnose(strings.NewReader(""), "vazio.log")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if d.HasFindings() {
		t.Errorf("achados em log vazio: %+v", d.Findings)
	}
	if d.LinesScanned != 0 {
		t.Errorf("LinesScanned = %d, quero 0", d.LinesScanned)
	}
	// Precisa serializar como [] e não null para quem consome o JSON.
	if d.Findings == nil {
		t.Error("Findings = nil, quero slice vazio")
	}
}

func TestDiagnoseRegistraLinhaEEvidencia(t *testing.T) {
	log := "primeira linha\n│ A variable named \"setor\" was assigned on the command line, but the root"

	d, err := Diagnose(strings.NewReader(log), "")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}

	f := d.Findings[0]
	if f.Line != 2 {
		t.Errorf("Line = %d, quero 2", f.Line)
	}
	// A moldura do Terraform sai da evidência.
	if strings.HasPrefix(f.Evidence, "│") {
		t.Errorf("evidência manteve a moldura: %q", f.Evidence)
	}
	if !strings.HasPrefix(f.Evidence, "A variable named") {
		t.Errorf("evidência = %q", f.Evidence)
	}
}

func TestStorageNameViolations(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"comprimento", "aplicacaodemo01frontdevstg", "fora da faixa"},
		{"curto demais", "ab", "fora da faixa"},
		{"maiúscula", "MeuStorage", "maiúscula"},
		{"caractere inválido", "meu-storage", "caractere inválido"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Join(storageNameViolations(tt.input), "; ")
			if !strings.Contains(got, tt.want) {
				t.Errorf("storageNameViolations(%q) = %q, quero conter %q", tt.input, got, tt.want)
			}
		})
	}

	if v := storageNameViolations("validname123"); len(v) != 0 {
		t.Errorf("nome válido acusou violações: %v", v)
	}
}

// Cada assinatura precisa de id, título, causa e ação — sem isso o achado não
// ajuda ninguém.
func TestCatalogoCompleto(t *testing.T) {
	ids := map[string]bool{}

	for _, sig := range signatures {
		if sig.id == "" || sig.title == "" || sig.cause == "" || sig.action == "" {
			t.Errorf("assinatura incompleta: %+v", sig.id)
		}
		if sig.severity != SeverityError && sig.severity != SeverityWarning {
			t.Errorf("%s: severidade inválida %q", sig.id, sig.severity)
		}
		if ids[sig.id] {
			t.Errorf("id duplicado: %s", sig.id)
		}
		ids[sig.id] = true
	}
}
