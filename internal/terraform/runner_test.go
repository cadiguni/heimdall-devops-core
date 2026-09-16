package terraform

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// terraformDir prepara um módulo mínimo com backend local e já inicializado.
//
// Usa terraform_data, que é embutido: o init não baixa provider nenhum, então
// o teste não depende de rede nem de credencial.
func terraformDir(t *testing.T) string {
	t.Helper()

	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform não está no PATH")
	}

	dir := t.TempDir()
	main := `resource "terraform_data" "importado" {
  input = "x"
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(main), 0o600); err != nil {
		t.Fatalf("escrevendo main.tf: %v", err)
	}

	cmd := exec.Command("terraform", "init", "-input=false", "-no-color")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("terraform init: %v\n%s", err, out)
	}

	return dir
}

// Executa o terraform de verdade: é a única forma de provar que a ligação com
// o terraform-exec funciona.
func TestExecImporterImportaDeVerdade(t *testing.T) {
	dir := terraformDir(t)

	var stdout, stderr bytes.Buffer
	importer, err := NewExecImporter(dir, &stdout, &stderr)
	if err != nil {
		t.Fatalf("NewExecImporter: %v", err)
	}

	if err := importer.Import(context.Background(), "terraform_data.importado", "abc-123"); err != nil {
		t.Fatalf("Import: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	state, err := os.ReadFile(filepath.Join(dir, "terraform.tfstate"))
	if err != nil {
		t.Fatalf("lendo state: %v", err)
	}

	inspection, err := InspectState(bytes.NewReader(state), "terraform.tfstate")
	if err != nil {
		t.Fatalf("InspectState: %v", err)
	}
	if inspection.Managed != 1 {
		t.Fatalf("recursos no state = %d, quero 1", inspection.Managed)
	}
	if addr := inspection.Resources[0].Address; addr != "terraform_data.importado" {
		t.Errorf("endereço no state = %q", addr)
	}
}

// Endereço que não existe na configuração faz o terraform recusar; o erro
// precisa chegar a quem chamou, não sumir.
func TestExecImporterPropagaFalhaDoTerraform(t *testing.T) {
	dir := terraformDir(t)

	var stdout, stderr bytes.Buffer
	importer, err := NewExecImporter(dir, &stdout, &stderr)
	if err != nil {
		t.Fatalf("NewExecImporter: %v", err)
	}

	err = importer.Import(context.Background(), "terraform_data.nao_declarado", "abc-123")

	if err == nil {
		t.Fatal("erro = nil, quero a falha do terraform")
	}
	if !strings.Contains(err.Error(), "terraform import falhou") {
		t.Errorf("erro = %v", err)
	}
}

// estadoDe lê o state do diretório de trabalho.
func estadoDe(t *testing.T, dir string) *StateInspection {
	t.Helper()

	f, err := os.Open(filepath.Join(dir, "terraform.tfstate"))
	if err != nil {
		t.Fatalf("abrindo state: %v", err)
	}
	defer f.Close()

	inspection, err := InspectState(f, "terraform.tfstate")
	if err != nil {
		t.Fatalf("InspectState: %v", err)
	}
	return inspection
}

func TestExecStateRmRemoveDeVerdade(t *testing.T) {
	dir := terraformDir(t)

	var stdout, stderr bytes.Buffer
	runner, err := NewExecImporter(dir, &stdout, &stderr)
	if err != nil {
		t.Fatalf("NewExecImporter: %v", err)
	}

	if err := runner.Import(context.Background(), "terraform_data.importado", "abc-123"); err != nil {
		t.Fatalf("Import: %v\n%s", err, stderr.String())
	}
	if n := estadoDe(t, dir).Managed; n != 1 {
		t.Fatalf("recursos antes do rm = %d, quero 1", n)
	}

	if err := runner.StateRm(context.Background(), "terraform_data.importado"); err != nil {
		t.Fatalf("StateRm: %v\n%s", err, stderr.String())
	}

	if n := estadoDe(t, dir).Managed; n != 0 {
		t.Errorf("recursos depois do rm = %d, quero 0", n)
	}
}

func TestExecStateMvTrocaOEndereco(t *testing.T) {
	dir := terraformDir(t)

	// O destino precisa existir na configuração, senão o terraform recusa.
	main := `resource "terraform_data" "importado" {
  input = "x"
}

resource "terraform_data" "renomeado" {
  input = "x"
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(main), 0o600); err != nil {
		t.Fatalf("reescrevendo main.tf: %v", err)
	}

	var stdout, stderr bytes.Buffer
	runner, err := NewExecImporter(dir, &stdout, &stderr)
	if err != nil {
		t.Fatalf("NewExecImporter: %v", err)
	}

	if err := runner.Import(context.Background(), "terraform_data.importado", "abc-123"); err != nil {
		t.Fatalf("Import: %v\n%s", err, stderr.String())
	}

	if err := runner.StateMv(context.Background(), "terraform_data.importado", "terraform_data.renomeado"); err != nil {
		t.Fatalf("StateMv: %v\n%s", err, stderr.String())
	}

	inspection := estadoDe(t, dir)
	if inspection.Managed != 1 {
		t.Fatalf("recursos = %d, quero 1", inspection.Managed)
	}
	if addr := inspection.Resources[0].Address; addr != "terraform_data.renomeado" {
		t.Errorf("endereço depois do mv = %q, quero terraform_data.renomeado", addr)
	}
}

func TestExecStateRmPropagaFalhaDoTerraform(t *testing.T) {
	dir := terraformDir(t)

	var stdout, stderr bytes.Buffer
	runner, err := NewExecImporter(dir, &stdout, &stderr)
	if err != nil {
		t.Fatalf("NewExecImporter: %v", err)
	}

	err = runner.StateRm(context.Background(), "terraform_data.nao_existe")

	if err == nil {
		t.Fatal("erro = nil, quero a falha do terraform")
	}
	if !strings.Contains(err.Error(), "terraform state rm falhou") {
		t.Errorf("erro = %v", err)
	}
}

func TestNewExecImporterDiretorioInexistente(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform não está no PATH")
	}

	if _, err := NewExecImporter(filepath.Join(t.TempDir(), "nao-existe"), nil, nil); err == nil {
		t.Error("erro = nil, quero falha em diretório inexistente")
	}
}
