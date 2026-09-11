package pipeline

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func renderFixture(t *testing.T) string {
	t.Helper()

	var buf bytes.Buffer
	if err := WriteReport(&buf, diagnoseFixture(t)); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	return buf.String()
}

func TestWriteReportAgrupaPorAssinatura(t *testing.T) {
	out := renderFixture(t)

	// As 7 variáveis não declaradas viram um bloco com 7 ocorrências, não 7
	// blocos repetindo causa e ação.
	if strings.Count(out, "(undeclared-variable)") != 1 {
		t.Errorf("assinatura repetida em vez de agrupada:\n%s", out)
	}
	if !strings.Contains(out, "Ocorrências (7):") {
		t.Errorf("faltou a contagem de ocorrências agrupadas:\n%s", out)
	}
}

func TestWriteReportErrosAntesDeAvisos(t *testing.T) {
	out := renderFixture(t)

	primeiroErro := strings.Index(out, "[erro]")
	primeiroAviso := strings.Index(out, "[aviso]")

	if primeiroErro == -1 || primeiroAviso == -1 {
		t.Fatalf("faltou erro ou aviso na saída:\n%s", out)
	}
	if primeiroErro > primeiroAviso {
		t.Errorf("aviso apareceu antes do erro:\n%s", out)
	}
}

func TestWriteReportMostraComandos(t *testing.T) {
	out := renderFixture(t)

	if !strings.Contains(out, "Comandos sugeridos (confira antes de rodar):") {
		t.Errorf("faltou a seção de comandos:\n%s", out)
	}
	if !strings.Contains(out, "terraform import 'azurerm_resource_group.rg[0]'") {
		t.Errorf("faltou o comando de import montado:\n%s", out)
	}
}

func TestWriteReportSemAchados(t *testing.T) {
	var buf bytes.Buffer
	d := &Diagnosis{Source: "x.log", LinesScanned: 10, Findings: []Finding{}}

	if err := WriteReport(&buf, d); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Nenhuma falha conhecida") {
		t.Errorf("faltou a mensagem de nada reconhecido:\n%s", out)
	}
	// Não pode dar a entender que o log está limpo.
	if !strings.Contains(out, "não está no catálogo") {
		t.Errorf("a mensagem precisa deixar claro o limite do catálogo:\n%s", out)
	}
}

// Detalhes vêm de um mapa; a saída não pode mudar entre execuções.
func TestFormatDetailsEstavel(t *testing.T) {
	details := map[string]string{"zeta": "3", "alfa": "1", "meio": "2"}

	primeiro := formatDetails(details)
	for i := 0; i < 20; i++ {
		if got := formatDetails(details); got != primeiro {
			t.Fatalf("saída instável: %q vs %q", got, primeiro)
		}
	}
	if primeiro != "alfa=1  meio=2  zeta=3" {
		t.Errorf("formatDetails = %q", primeiro)
	}
	if formatDetails(nil) != "" {
		t.Error("mapa vazio deveria render string vazia")
	}
}

// Cortar por byte partiria um caractere acentuado ou de moldura ao meio.
func TestTruncatePorRunes(t *testing.T) {
	acentuado := strings.Repeat("ção", 100)

	got := truncate(acentuado, 10)

	if r := []rune(got); len(r) != 11 { // 10 + reticências
		t.Errorf("truncate devolveu %d runes, quero 11", len(r))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate = %q, esperava reticências", got)
	}
	if strings.Contains(got, "�") {
		t.Errorf("truncate quebrou um caractere: %q", got)
	}

	if curto := truncate("abc", 10); curto != "abc" {
		t.Errorf("string curta foi alterada: %q", curto)
	}
}

func TestWriteReportPropagaErroDeEscrita(t *testing.T) {
	if err := WriteReport(escritorQueFalha{}, diagnoseFixture(t)); err == nil {
		t.Error("WriteReport não propagou o erro de escrita")
	}
}

type escritorQueFalha struct{}

func (escritorQueFalha) Write([]byte) (int, error) { return 0, os.ErrClosed }
