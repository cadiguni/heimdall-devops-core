package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

const pipelineFixtureDir = "../../internal/pipeline/testdata/"

func TestDiagnoseRelatorioDeTexto(t *testing.T) {
	out, err := runCLI(t, "", "pipeline", "diagnose", "--log", pipelineFixtureDir+"pipeline_log_misto.txt")
	if err != nil {
		t.Fatalf("erro = %v", err)
	}

	for _, want := range []string{
		"[erro]",
		"resource-already-exists",
		"terraform import 'azurerm_resource_group.rg[0]'",
		"Ocorrências (7):",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q:\n%s", want, out)
		}
	}
}

func TestDiagnoseSaidaJSON(t *testing.T) {
	out, err := runCLI(t, "",
		"pipeline", "diagnose", "--log", pipelineFixtureDir+"pipeline_log_misto.txt", "-o", "json",
	)
	if err != nil {
		t.Fatalf("erro = %v", err)
	}

	var diagnosis struct {
		LinesScanned int `json:"lines_scanned"`
		Findings     []struct {
			Signature string   `json:"signature"`
			Severity  string   `json:"severity"`
			Line      int      `json:"line"`
			Commands  []string `json:"commands"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(out), &diagnosis); err != nil {
		t.Fatalf("saída não é JSON válido: %v\n%s", err, out)
	}

	if diagnosis.LinesScanned == 0 {
		t.Error("lines_scanned = 0")
	}
	if len(diagnosis.Findings) != 21 {
		t.Errorf("achados = %d, quero 21", len(diagnosis.Findings))
	}

	var comImport int
	for _, f := range diagnosis.Findings {
		if len(f.Commands) > 0 {
			comImport++
		}
	}
	if comImport != 2 {
		t.Errorf("achados com comando = %d, quero 2", comImport)
	}
}

func TestDiagnoseLeStdin(t *testing.T) {
	log, err := os.ReadFile(pipelineFixtureDir + "pipeline_log_misto.txt")
	if err != nil {
		t.Fatalf("lendo fixture: %v", err)
	}

	out, err := runCLI(t, string(log), "pipeline", "diagnose", "--log", "-")
	if err != nil {
		t.Fatalf("erro = %v", err)
	}
	if !strings.Contains(out, "Log: stdin") {
		t.Errorf("origem deveria ser stdin:\n%s", out)
	}
	if !strings.Contains(out, "undeclared-variable") {
		t.Errorf("log do stdin não foi analisado:\n%s", out)
	}
}

// Diagnose é investigativo, não é gate: reconhecer falhas não é motivo para
// sair com código diferente de zero.
func TestDiagnoseSempreSaiZeroQuandoFunciona(t *testing.T) {
	if _, err := runCLI(t, "", "pipeline", "diagnose", "--log", pipelineFixtureDir+"pipeline_log_misto.txt"); err != nil {
		t.Errorf("erro = %v, quero nil mesmo com achados", err)
	}
}

func TestDiagnoseErrosSaemCom1(t *testing.T) {
	tests := map[string][]string{
		"arquivo inexistente": {"pipeline", "diagnose", "--log", pipelineFixtureDir + "nao_existe.log"},
		"formato inválido":    {"pipeline", "diagnose", "--log", pipelineFixtureDir + "pipeline_log_misto.txt", "-o", "xml"},
		"sem --log":           {"pipeline", "diagnose"},
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := runCLI(t, "", args...)
			if err == nil {
				t.Fatal("erro = nil, quero falha")
			}
			if got := exitCodeFor(err); got != 1 {
				t.Errorf("código de saída = %d, quero 1", got)
			}
		})
	}
}

func TestPipelineMostraHelpSemArgs(t *testing.T) {
	out, err := runCLI(t, "", "pipeline")
	if err != nil {
		t.Errorf("erro = %v", err)
	}
	if !strings.Contains(out, "Usage:") {
		t.Errorf("não mostrou o help:\n%s", out)
	}
}
