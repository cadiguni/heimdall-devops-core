package pipeline

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func renderLista(t *testing.T) string {
	t.Helper()

	inv := listar(t, &fakeGroupLister{groups: gruposDeExemplo()})
	inv.Project = "https://dev.azure.com/minhaorg/MeuProjeto"

	var buf bytes.Buffer
	if err := WriteVariableGroups(&buf, inv); err != nil {
		t.Fatalf("WriteVariableGroups: %v", err)
	}
	return buf.String()
}

func renderDetalhe(t *testing.T, reveal bool) string {
	t.Helper()

	var buf bytes.Buffer
	if err := WriteVariableGroupDetail(&buf, encontrar(t, "terraform-dev", reveal)); err != nil {
		t.Fatalf("WriteVariableGroupDetail: %v", err)
	}
	return buf.String()
}

func TestWriteVariableGroups(t *testing.T) {
	out := renderLista(t)

	for _, want := range []string{
		"https://dev.azure.com/minhaorg/MeuProjeto", // sempre diz contra o quê
		"terraform-dev",
		"cofre-prod",
		// "Vsts" é nome interno e não diz nada a quem lê.
		"comum",
		"Key Vault",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q:\n%s", want, out)
		}
	}

	// A listagem nunca toca em valor.
	if strings.Contains(out, "dev/app.tfstate") {
		t.Errorf("a listagem vazou valor de variável:\n%s", out)
	}
}

func TestWriteVariableGroupsVazio(t *testing.T) {
	var buf bytes.Buffer
	inv := &VariableGroupInventory{Project: "https://x/y", Groups: []VariableGroupSummary{}}

	if err := WriteVariableGroups(&buf, inv); err != nil {
		t.Fatalf("WriteVariableGroups: %v", err)
	}
	if !strings.Contains(buf.String(), "Nenhum Variable Group encontrado.") {
		t.Errorf("faltou a mensagem de projeto sem grupos:\n%s", buf.String())
	}
}

func TestWriteVariableGroupDetailSemValores(t *testing.T) {
	out := renderDetalhe(t, false)

	for _, want := range []string{
		"terraform-dev",
		"AMBIENTE",
		"ARM_CLIENT_SECRET",
		"SIM", // marca a secreta
		"Use --show-values",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q:\n%s", want, out)
		}
	}

	// O ponto central: nenhum valor sem pedido.
	for _, valor := range []string{"dev/app.tfstate", "VALOR"} {
		if strings.Contains(out, valor) {
			t.Errorf("saída vazou %q sem --show-values:\n%s", valor, out)
		}
	}
}

func TestWriteVariableGroupDetailComValores(t *testing.T) {
	out := renderDetalhe(t, true)

	if !strings.Contains(out, "dev/app.tfstate") {
		t.Errorf("faltou o valor da variável não secreta:\n%s", out)
	}
	// Secreta continua sem valor, e a saída explica por quê.
	if !strings.Contains(out, "não devolvida pela API") {
		t.Errorf("faltou explicar por que a secreta não tem valor:\n%s", out)
	}
	// E o aviso de não mandar isso para log.
	if !strings.Contains(out, "não deve ir para log de pipeline") {
		t.Errorf("faltou o aviso sobre log:\n%s", out)
	}
}

// Grupo em que toda variável é secreta não deve sugerir --show-values: não há
// valor nenhum a revelar.
func TestWriteVariableGroupDetailTodasSecretasNaoSugereFlag(t *testing.T) {
	d, err := FindVariableGroup(context.Background(), &fakeGroupLister{groups: gruposDeExemplo()}, "cofre-prod", false)
	if err != nil {
		t.Fatalf("FindVariableGroup: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteVariableGroupDetail(&buf, d); err != nil {
		t.Fatalf("WriteVariableGroupDetail: %v", err)
	}

	if strings.Contains(buf.String(), "--show-values") {
		t.Errorf("não faz sentido sugerir a flag num grupo só de secretas:\n%s", buf.String())
	}
}

func TestWriteVariableGroupDetailSemVariaveis(t *testing.T) {
	var buf bytes.Buffer
	d := &VariableGroupDetail{
		VariableGroupSummary: VariableGroupSummary{ID: 1, Name: "vazio", Type: "Vsts"},
		Variables:            []Variable{},
	}

	if err := WriteVariableGroupDetail(&buf, d); err != nil {
		t.Fatalf("WriteVariableGroupDetail: %v", err)
	}
	if !strings.Contains(buf.String(), "não tem variáveis") {
		t.Errorf("faltou a mensagem de grupo vazio:\n%s", buf.String())
	}
}

func TestWriteVariablesPropagamErroDeEscrita(t *testing.T) {
	inv := listar(t, &fakeGroupLister{groups: gruposDeExemplo()})
	if err := WriteVariableGroups(escritorQueFalha{}, inv); err == nil {
		t.Error("WriteVariableGroups não propagou o erro de escrita")
	}

	if err := WriteVariableGroupDetail(escritorQueFalha{}, encontrar(t, "terraform-dev", false)); err == nil {
		t.Error("WriteVariableGroupDetail não propagou o erro de escrita")
	}
}
