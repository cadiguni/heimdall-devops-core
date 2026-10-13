package main

import (
	"strings"
	"testing"
)

func TestCoverageValidaAntesDeIrAoAzure(t *testing.T) {
	backend := []string{"--account", "st", "--container", "c"}

	casos := map[string][]string{
		"sem --subscription": append([]string{"terraform", "states", "coverage"}, backend...),
		"formato inválido": append([]string{"terraform", "states", "coverage", "--subscription", "x"},
			append(backend, "-o", "yaml")...),
		"com argumento":   append([]string{"terraform", "states", "coverage", "sobrando", "--subscription", "x"}, backend...),
		"sem --container": {"terraform", "states", "coverage", "--subscription", "x", "--account", "st"},
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

// O coverage não precisa de --key: ele varre o container inteiro. Exigir a
// chave seria reduzir a varredura a um state.
func TestCoverageNaoExigeKey(t *testing.T) {
	out, err := runCLI(t, "", "terraform", "states", "coverage", "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(out, "--subscription") || !strings.Contains(out, "--prefix") {
		t.Errorf("ajuda sem as flags próprias:\n%s", out)
	}
}

func TestCoverageApareceNoGrupoStates(t *testing.T) {
	out, err := runCLI(t, "", "terraform", "states", "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(out, "coverage") {
		t.Errorf("coverage não aparece no grupo states:\n%s", out)
	}
}
