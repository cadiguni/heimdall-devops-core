package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReferencesValidaAntesDeIrAoAzure(t *testing.T) {
	casos := map[string][]string{
		"sem o que procurar":      {"pipeline", "references", "--org", "o", "--project", "p"},
		"formato inválido":        {"pipeline", "references", "--subscription", "x", "--org", "o", "--project", "p", "-o", "yaml"},
		"skip-devops sem --chdir": {"pipeline", "references", "--subscription", "x", "--skip-devops"},
		"com argumento sobrando":  {"pipeline", "references", "sobrando", "--subscription", "x", "--org", "o", "--project", "p"},
		"sem --org":               {"pipeline", "references", "--subscription", "x", "--project", "p"},
	}

	for name, args := range casos {
		t.Run(name, func(t *testing.T) {
			_, err := runCLI(t, "", args...)
			if err == nil {
				t.Fatal("erro = nil, quero falha")
			}
			if strings.Contains(err.Error(), "credencial") || strings.Contains(err.Error(), "DefaultAzureCredential") {
				t.Errorf("chegou a tentar autenticar: %v", err)
			}
		})
	}
}

// Com --skip-devops o comando funciona sem nuvem nenhuma, varrendo só o
// repositório. É o modo que dá para rodar em qualquer máquina.
func TestReferencesVarreRepositorioSemNuvem(t *testing.T) {
	dir := t.TempDir()
	escrever(t, filepath.Join(dir, "main.tf"),
		"provider \"azurerm\" {\n  subscription_id = \"92dbecc2-c36d-4af2-887d-3681969e5850\"\n}\n")
	escrever(t, filepath.Join(dir, "README.md"),
		"a assinatura 92dbecc2-c36d-4af2-887d-3681969e5850 aparece aqui também\n")
	escrever(t, filepath.Join(dir, ".terraform", "modules", "x.tf"),
		"subscription_id = \"92dbecc2-c36d-4af2-887d-3681969e5850\"\n")

	out, err := runCLI(t, "", "pipeline", "references",
		"--subscription", "92dbecc2-c36d-4af2-887d-3681969e5850",
		"--skip-devops", "--chdir", dir)
	if err != nil {
		t.Fatalf("erro = %v", err)
	}

	if !strings.Contains(out, "main.tf:2") {
		t.Errorf("não achou no .tf:\n%s", out)
	}
	if !strings.Contains(out, "subscription_id") {
		t.Errorf("não trouxe o identificador:\n%s", out)
	}
	// .md não está na lista de extensões varridas.
	if strings.Contains(out, "README.md") {
		t.Errorf("varreu arquivo fora da lista:\n%s", out)
	}
	// .terraform é cache: achado lá não é lugar de alterar nada.
	if strings.Contains(out, ".terraform") && !strings.Contains(out, "pulados") {
		t.Errorf("varreu o cache do terraform:\n%s", out)
	}
}

func TestReferencesChdirInexistenteFalhaClaro(t *testing.T) {
	_, err := runCLI(t, "", "pipeline", "references",
		"--subscription", "x", "--skip-devops", "--chdir", filepath.Join(t.TempDir(), "nao-existe"))
	if err == nil {
		t.Fatal("diretório inexistente passou")
	}
	if !strings.Contains(err.Error(), "nao-existe") {
		t.Errorf("o erro não diz qual caminho: %v", err)
	}
}

func escrever(t *testing.T, caminho, conteudo string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(caminho), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(caminho, []byte(conteudo), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
