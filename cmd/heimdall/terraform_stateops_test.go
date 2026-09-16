package main

import (
	"strings"
	"testing"
)

func TestStateOpsValidamAntesDeIrAoAzure(t *testing.T) {
	backend := []string{"--account", "st", "--container", "c", "--key", "dev/app.tfstate"}

	casos := map[string][]string{
		"rm com formato inválido": append([]string{"terraform", "states", "rm", "a.b"}, append(backend, "-o", "yaml")...),
		"rm com json e apply":     append([]string{"terraform", "states", "rm", "a.b"}, append(backend, "-o", "json", "--apply")...),
		"mv com formato inválido": append([]string{"terraform", "states", "mv", "a.b", "c.d"}, append(backend, "-o", "yaml")...),
		"mv com json e apply":     append([]string{"terraform", "states", "mv", "a.b", "c.d"}, append(backend, "-o", "json", "--apply")...),
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
			if got := exitCodeFor(err); got != 1 {
				t.Errorf("código de saída = %d, quero 1", got)
			}
		})
	}
}

func TestStateOpsExigemArgumentosEFlags(t *testing.T) {
	backend := []string{"--account", "st", "--container", "c", "--key", "dev/app.tfstate"}

	casos := map[string][]string{
		"rm sem endereço":    {"terraform", "states", "rm"},
		"rm com extra":       append([]string{"terraform", "states", "rm", "a.b", "sobrando"}, backend...),
		"rm sem --key":       {"terraform", "states", "rm", "a.b", "--account", "st", "--container", "c"},
		"mv sem destino":     append([]string{"terraform", "states", "mv", "a.b"}, backend...),
		"mv com extra":       append([]string{"terraform", "states", "mv", "a.b", "c.d", "sobrando"}, backend...),
		"mv sem --container": {"terraform", "states", "mv", "a.b", "c.d", "--account", "st", "--key", "dev/app.tfstate"},
	}

	for name, args := range casos {
		t.Run(name, func(t *testing.T) {
			if _, err := runCLI(t, "", args...); err == nil {
				t.Error("erro = nil, quero falha")
			}
		})
	}
}

// O terraform chama de "terraform state rm"; quem digitar no singular precisa
// chegar no mesmo lugar.
func TestStatesAceitaAliasNoSingular(t *testing.T) {
	for _, grupo := range []string{"states", "state"} {
		t.Run(grupo, func(t *testing.T) {
			out, err := runCLI(t, "", "terraform", grupo, "--help")
			if err != nil {
				t.Fatalf("erro = %v", err)
			}
			for _, sub := range []string{"list", "show", "rm", "mv"} {
				if !strings.Contains(out, sub) {
					t.Errorf("help não lista o subcomando %q:\n%s", sub, out)
				}
			}
		})
	}
}

func TestStateOpsDryRunEhOPadrao(t *testing.T) {
	for _, sub := range []string{"rm", "mv"} {
		t.Run(sub, func(t *testing.T) {
			out, err := runCLI(t, "", "terraform", "states", sub, "--help")
			if err != nil {
				t.Fatalf("erro = %v", err)
			}
			if !strings.Contains(out, "sem isto é dry-run") {
				t.Errorf("help não deixa claro que o padrão é dry-run:\n%s", out)
			}
		})
	}
}

// O rm precisa dizer no help que a infraestrutura sobrevive: é a diferença
// entre 'state rm' e 'destroy', e confundir as duas é caro.
func TestStatesRmExplicaQueNaoDestroi(t *testing.T) {
	out, err := runCLI(t, "", "terraform", "states", "rm", "--help")
	if err != nil {
		t.Fatalf("erro = %v", err)
	}

	if !strings.Contains(out, "continua existindo") {
		t.Errorf("help não diz que a infraestrutura sobrevive:\n%s", out)
	}
}
