package main

import (
	"strings"
	"testing"
)

// A validação dos argumentos acontece antes de qualquer ida ao Azure: errar a
// chamada não deve custar uma tentativa de autenticação para descobrir.
func TestImportValidaAntesDeIrAoAzure(t *testing.T) {
	base := []string{
		"terraform", "import", "azurerm_resource_group.rg", "/subscriptions/x/resourceGroups/y",
		"--account", "st", "--container", "c", "--key", "dev/app.tfstate",
	}

	casos := map[string][]string{
		"formato de saída inválido": append(append([]string{}, base...), "-o", "yaml"),
		"json com apply":            append(append([]string{}, base...), "-o", "json", "--apply"),
	}

	for name, args := range casos {
		t.Run(name, func(t *testing.T) {
			_, err := runCLI(t, "", args...)
			if err == nil {
				t.Fatal("erro = nil, quero falha")
			}
			// Se tivesse ido ao Azure, o erro seria de credencial.
			if strings.Contains(err.Error(), "credencial") || strings.Contains(err.Error(), "DefaultAzureCredential") {
				t.Errorf("chegou a tentar autenticar: %v", err)
			}
			if got := exitCodeFor(err); got != 1 {
				t.Errorf("código de saída = %d, quero 1", got)
			}
		})
	}
}

func TestImportExigeArgumentosEFlags(t *testing.T) {
	casos := map[string][]string{
		"sem argumentos":  {"terraform", "import"},
		"só o endereço":   {"terraform", "import", "azurerm_resource_group.rg"},
		"argumento extra": {"terraform", "import", "a.b", "id", "sobrando"},
		"sem --key": {"terraform", "import", "a.b", "id",
			"--account", "st", "--container", "c"},
		"sem --account": {"terraform", "import", "a.b", "id",
			"--container", "c", "--key", "dev/app.tfstate"},
	}

	for name, args := range casos {
		t.Run(name, func(t *testing.T) {
			if _, err := runCLI(t, "", args...); err == nil {
				t.Error("erro = nil, quero falha")
			}
		})
	}
}

// O dry-run é o padrão: --apply precisa ser digitado.
func TestImportDryRunEhOPadrao(t *testing.T) {
	out, err := runCLI(t, "", "terraform", "import", "--help")
	if err != nil {
		t.Fatalf("erro = %v", err)
	}

	if !strings.Contains(out, "--apply") {
		t.Errorf("help não menciona --apply:\n%s", out)
	}
	if !strings.Contains(out, "sem isto é dry-run") {
		t.Errorf("help não deixa claro que o padrão é dry-run:\n%s", out)
	}
}
