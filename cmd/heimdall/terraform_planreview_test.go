package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"testing"
)

// As fixtures vivem junto do pacote que as analisa; aqui só reusamos.
const fixtureDir = "../../internal/terraform/testdata/"

// runCLI executa o comando raiz com os argumentos dados e devolve stdout e o
// erro final — o mesmo que o main() converte em código de saída.
func runCLI(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	cmd := newRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))

	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestPlanReviewSaiCom2QuandoHaDestruicao(t *testing.T) {
	out, err := runCLI(t, "", "terraform", "plan-review", "--plan-json", fixtureDir+"plan_mixed.json")

	var exitErr *exitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("erro = %v, quero *exitError", err)
	}
	if exitErr.code != exitCodeDestructive {
		t.Errorf("código de saída = %d, quero %d", exitErr.code, exitCodeDestructive)
	}
	if exitCodeFor(err) != exitCodeDestructive {
		t.Errorf("exitCodeFor = %d, quero %d", exitCodeFor(err), exitCodeDestructive)
	}
	if !strings.Contains(out, "terraform_data.to_delete") {
		t.Errorf("relatório não listou o recurso destruído:\n%s", out)
	}
}

func TestPlanReviewFailOnDestroyDesligado(t *testing.T) {
	out, err := runCLI(t, "",
		"terraform", "plan-review",
		"--plan-json", fixtureDir+"plan_mixed.json",
		"--fail-on-destroy=false",
	)

	if err != nil {
		t.Fatalf("erro = %v, quero nil com o gate desligado", err)
	}
	// O relatório continua mostrando a destruição; só não reprova.
	if !strings.Contains(out, "Operações destrutivas") {
		t.Errorf("relatório deveria continuar listando as destrutivas:\n%s", out)
	}
}

func TestPlanReviewPlanoLimpoSaiCom0(t *testing.T) {
	if _, err := runCLI(t, "", "terraform", "plan-review", "--plan-json", fixtureDir+"plan_no_changes.json"); err != nil {
		t.Fatalf("erro = %v, quero nil", err)
	}
}

func TestPlanReviewSaiCom3QuandoPlanoIncompleto(t *testing.T) {
	// A fixture com -target não tem nenhuma destrutiva: sem o gate de plano
	// incompleto ela passaria como se estivesse tudo bem.
	out, err := runCLI(t, "", "terraform", "plan-review", "--plan-json", fixtureDir+"plan_targeted_incomplete.json")

	if got := exitCodeFor(err); got != exitCodeIncomplete {
		t.Fatalf("código de saída = %d, quero %d (erro: %v)", got, exitCodeIncomplete, err)
	}
	if !strings.Contains(out, "plano incompleto") {
		t.Errorf("relatório não avisou do plano incompleto:\n%s", out)
	}
}

func TestPlanReviewFailOnIncompleteDesligado(t *testing.T) {
	if _, err := runCLI(t, "",
		"terraform", "plan-review",
		"--plan-json", fixtureDir+"plan_targeted_incomplete.json",
		"--fail-on-incomplete=false",
	); err != nil {
		t.Fatalf("erro = %v, quero nil com o gate desligado", err)
	}
}

// Quando as duas condições valem, quem manda no código de saída é a destruição.
func TestPlanReviewDestrutivaTemPrecedenciaSobreIncompleto(t *testing.T) {
	_, err := runCLI(t, "", "terraform", "plan-review", "--plan-json", fixtureDir+"plan_incomplete_destructive.json")

	if got := exitCodeFor(err); got != exitCodeDestructive {
		t.Errorf("código de saída = %d, quero %d (destrutiva antes de incompleto)", got, exitCodeDestructive)
	}
}

// Com --fail-on-destroy desligado, o plano incompleto ainda reprova por conta
// própria — os dois gates são independentes.
func TestPlanReviewIncompletoReprovaMesmoSemGateDeDestruicao(t *testing.T) {
	_, err := runCLI(t, "",
		"terraform", "plan-review",
		"--plan-json", fixtureDir+"plan_incomplete_destructive.json",
		"--fail-on-destroy=false",
	)

	if got := exitCodeFor(err); got != exitCodeIncomplete {
		t.Errorf("código de saída = %d, quero %d", got, exitCodeIncomplete)
	}
}

func TestPlanReviewSaidaJSON(t *testing.T) {
	out, err := runCLI(t, "",
		"terraform", "plan-review",
		"--plan-json", fixtureDir+"plan_mixed.json",
		"--output", "json",
		"--fail-on-destroy=false",
	)
	if err != nil {
		t.Fatalf("erro = %v", err)
	}

	var review struct {
		Summary struct {
			Delete  int `json:"delete"`
			Replace int `json:"replace"`
		} `json:"summary"`
		Destructive []struct {
			Address string `json:"address"`
			Kind    string `json:"kind"`
		} `json:"destructive"`
	}
	if err := json.Unmarshal([]byte(out), &review); err != nil {
		t.Fatalf("saída não é JSON válido: %v\n%s", err, out)
	}

	if review.Summary.Delete != 1 || review.Summary.Replace != 1 {
		t.Errorf("resumo JSON inesperado: %+v", review.Summary)
	}
	if len(review.Destructive) != 2 {
		t.Errorf("destrutivas no JSON = %d, quero 2", len(review.Destructive))
	}
}

func TestPlanReviewLeStdin(t *testing.T) {
	plano, err := os.ReadFile(fixtureDir + "plan_mixed.json")
	if err != nil {
		t.Fatalf("lendo fixture: %v", err)
	}

	out, err := runCLI(t, string(plano),
		"terraform", "plan-review", "--plan-json", "-", "--fail-on-destroy=false",
	)
	if err != nil {
		t.Fatalf("erro = %v", err)
	}
	if !strings.Contains(out, "terraform_data.to_replace") {
		t.Errorf("plano do stdin não foi analisado:\n%s", out)
	}
}

func TestPlanReviewErrosSaemCom1(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"arquivo inexistente", []string{"terraform", "plan-review", "--plan-json", fixtureDir + "nao_existe.json"}},
		{"formato de saída inválido", []string{"terraform", "plan-review", "--plan-json", fixtureDir + "plan_mixed.json", "--output", "yaml"}},
		{"sem --plan-json", []string{"terraform", "plan-review"}},
		{"argumento posicional inesperado", []string{"terraform", "plan-review", "sobrando", "--plan-json", fixtureDir + "plan_mixed.json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runCLI(t, "", tt.args...)
			if err == nil {
				t.Fatal("erro = nil, quero falha")
			}
			if got := exitCodeFor(err); got != 1 {
				t.Errorf("código de saída = %d, quero 1", got)
			}
		})
	}
}

// 'heimdall ... | head' fecha o pipe antes do fim da saída. Isso não é falha:
// quem lia recebeu o que queria.
func TestExitCodeForPipeFechado(t *testing.T) {
	casos := map[string]error{
		"EPIPE":      syscall.EPIPE,
		"embrulhado": fmt.Errorf("escrevendo relatório: %w", syscall.EPIPE),
		"PathError":  &fs.PathError{Op: "write", Path: "/dev/stdout", Err: syscall.EPIPE},
	}

	for name, err := range casos {
		t.Run(name, func(t *testing.T) {
			if got := exitCodeFor(err); got != 0 {
				t.Errorf("exitCodeFor = %d, quero 0", got)
			}
		})
	}

	if isBrokenPipe(errors.New("outro erro qualquer")) {
		t.Error("isBrokenPipe classificou erro comum como pipe fechado")
	}
}

// Com o pipe fechado o relatório sai truncado, mas o gate continua mandando no
// código de saída — importante sob 'set -o pipefail'.
func TestPlanReviewGateValeComPipeFechado(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"terraform", "plan-review", "--plan-json", fixtureDir + "plan_mixed.json"})
	cmd.SetOut(escritorComPipeFechado{})
	cmd.SetErr(io.Discard)

	err := cmd.ExecuteContext(context.Background())

	if got := exitCodeFor(err); got != exitCodeDestructive {
		t.Errorf("código de saída = %d, quero %d", got, exitCodeDestructive)
	}
}

type escritorComPipeFechado struct{}

func (escritorComPipeFechado) Write([]byte) (int, error) {
	return 0, &fs.PathError{Op: "write", Path: "/dev/stdout", Err: syscall.EPIPE}
}

func TestExitCodeForCancelamento(t *testing.T) {
	if got := exitCodeFor(context.Canceled); got != 130 {
		t.Errorf("exitCodeFor(context.Canceled) = %d, quero 130", got)
	}
}

func TestComandosMostramHelpSemArgs(t *testing.T) {
	for _, args := range [][]string{{}, {"terraform"}} {
		out, err := runCLI(t, "", args...)
		if err != nil {
			t.Errorf("%v: erro = %v", args, err)
		}
		if !strings.Contains(out, "Usage:") {
			t.Errorf("%v: não mostrou o help:\n%s", args, out)
		}
	}
}
