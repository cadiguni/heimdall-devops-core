package main

import (
	"strings"
	"testing"
)

func TestVariablesValidaAntesDeIrAoDevOps(t *testing.T) {
	casos := map[string][]string{
		"list com formato inválido": {"pipeline", "variables", "list", "--org", "o", "--project", "p", "-o", "yaml"},
		"show com formato inválido": {"pipeline", "variables", "show", "g", "--org", "o", "--project", "p", "-o", "yaml"},
	}

	for name, args := range casos {
		t.Run(name, func(t *testing.T) {
			_, err := runCLI(t, "", args...)
			if err == nil {
				t.Fatal("erro = nil, quero falha")
			}
			if !strings.Contains(err.Error(), "formato de saída inválido") {
				t.Errorf("erro = %v", err)
			}
			if got := exitCodeFor(err); got != 1 {
				t.Errorf("código de saída = %d, quero 1", got)
			}
		})
	}
}

func TestVariablesExigemArgumentosEFlags(t *testing.T) {
	casos := map[string][]string{
		"list sem --org":     {"pipeline", "variables", "list", "--project", "p"},
		"list sem --project": {"pipeline", "variables", "list", "--org", "o"},
		"list com extra":     {"pipeline", "variables", "list", "sobrando", "--org", "o", "--project", "p"},
		"show sem nome":      {"pipeline", "variables", "show", "--org", "o", "--project", "p"},
		"show com extra":     {"pipeline", "variables", "show", "a", "b", "--org", "o", "--project", "p"},
	}

	for name, args := range casos {
		t.Run(name, func(t *testing.T) {
			if _, err := runCLI(t, "", args...); err == nil {
				t.Error("erro = nil, quero falha")
			}
		})
	}
}

// O --show-values só existe no show: a listagem nunca lê valor.
func TestShowValuesSoExisteNoShow(t *testing.T) {
	out, err := runCLI(t, "", "pipeline", "variables", "show", "--help")
	if err != nil {
		t.Fatalf("erro = %v", err)
	}
	if !strings.Contains(out, "--show-values") {
		t.Errorf("show deveria ter --show-values:\n%s", out)
	}

	out, err = runCLI(t, "", "pipeline", "variables", "list", "--help")
	if err != nil {
		t.Fatalf("erro = %v", err)
	}
	if strings.Contains(out, "--show-values") {
		t.Errorf("list não deveria ter --show-values:\n%s", out)
	}
}

// O help precisa deixar claro que não há caminho de PAT, e por quê.
func TestVariablesHelpExplicaAusenciaDePAT(t *testing.T) {
	out, err := runCLI(t, "", "pipeline", "variables", "--help")
	if err != nil {
		t.Fatalf("erro = %v", err)
	}

	if !strings.Contains(out, "PAT") {
		t.Errorf("help não menciona PAT:\n%s", out)
	}
	if !strings.Contains(out, "az login") {
		t.Errorf("help não diz como autenticar:\n%s", out)
	}
}

func TestVariablesAceitaAliasVars(t *testing.T) {
	for _, grupo := range []string{"variables", "vars"} {
		t.Run(grupo, func(t *testing.T) {
			out, err := runCLI(t, "", "pipeline", grupo, "--help")
			if err != nil {
				t.Fatalf("erro = %v", err)
			}
			for _, sub := range []string{"list", "show"} {
				if !strings.Contains(out, sub) {
					t.Errorf("help não lista %q:\n%s", sub, out)
				}
			}
		})
	}
}
