package terraform

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func renderFixture(t *testing.T, name string) string {
	t.Helper()

	var buf bytes.Buffer
	if err := WriteTextReport(&buf, ReviewPlan(loadFixture(t, name))); err != nil {
		t.Fatalf("WriteTextReport: %v", err)
	}
	return buf.String()
}

func TestWriteTextReportMixed(t *testing.T) {
	out := renderFixture(t, "plan_mixed.json")

	for _, want := range []string{
		"Operações destrutivas (2)",
		"destruir",
		"terraform_data.to_delete",
		"recriar",
		"terraform_data.to_replace",
		"sem bloco de configuração correspondente",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q:\n%s", want, out)
		}
	}

	// Recursos não destrutivos não são listados individualmente, só contados.
	if strings.Contains(out, "terraform_data.to_create") {
		t.Errorf("recurso não destrutivo apareceu na lista:\n%s", out)
	}
}

func TestWriteTextReportNoChanges(t *testing.T) {
	out := renderFixture(t, "plan_no_changes.json")

	if !strings.Contains(out, "Nenhuma mudança planejada.") {
		t.Errorf("faltou a mensagem de plano vazio:\n%s", out)
	}
	if strings.Contains(out, "Operações destrutivas") {
		t.Errorf("plano vazio não deveria ter seção de destrutivas:\n%s", out)
	}
}

func TestWriteTextReportAvisaPlanoIncompleto(t *testing.T) {
	out := renderFixture(t, "plan_targeted_incomplete.json")

	if !strings.Contains(out, "plano incompleto") {
		t.Errorf("faltou o aviso de plano incompleto:\n%s", out)
	}
}

func TestWriteTextReportSemDestrutivas(t *testing.T) {
	var buf bytes.Buffer
	review := &PlanReview{Summary: Summary{Create: 2}, Destructive: []ResourceChange{}}
	if err := WriteTextReport(&buf, review); err != nil {
		t.Fatalf("WriteTextReport: %v", err)
	}

	if !strings.Contains(buf.String(), "Nenhuma operação destrutiva.") {
		t.Errorf("faltou a confirmação de ausência de destrutivas:\n%s", buf.String())
	}
}

// Erro de escrita precisa ser propagado, não engolido: se o relatório saiu pela
// metade, o pipeline não pode achar que passou limpo.
type falhaNoWrite struct{}

func (falhaNoWrite) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestWriteTextReportPropagaErroDeEscrita(t *testing.T) {
	review := ReviewPlan(loadFixture(t, "plan_mixed.json"))

	if err := WriteTextReport(falhaNoWrite{}, review); err == nil {
		t.Error("WriteTextReport não propagou o erro de escrita")
	}
}
