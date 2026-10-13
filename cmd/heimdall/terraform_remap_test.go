package main

import (
	"strings"
	"testing"
)

func TestRemapValidaAntesDeIrAoAzure(t *testing.T) {
	backend := []string{"--account", "st", "--container", "c", "--key", "dev/app.tfstate"}
	assinaturas := []string{
		"--from", "11111111-1111-1111-1111-111111111111",
		"--to", "22222222-2222-2222-2222-222222222222",
	}

	casos := map[string][]string{
		"formato inválido": join("terraform", "states", "remap", backend, assinaturas, []string{"-o", "yaml"}),
		"json com apply":   join("terraform", "states", "remap", backend, assinaturas, []string{"-o", "json", "--apply"}),
		"sem --key":        {"terraform", "states", "remap", "--account", "st", "--container", "c", "--from", "a", "--to", "b"},
		"com argumento":    join("terraform", "states", "remap", []string{"sobrando"}, backend, assinaturas),
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

// --from e --to são validados no domínio, depois da resolução do perfil, e não
// por MarkFlagRequired: o cobra valida antes do RunE, ou seja antes de o
// --profile ter chance de preencher o backend.
func TestRemapExigeAsDuasAssinaturas(t *testing.T) {
	backend := []string{"--account", "st", "--container", "c", "--key", "dev/app.tfstate"}

	casos := map[string][]string{
		"sem --from":  join("terraform", "states", "remap", backend, []string{"--to", "b"}),
		"sem --to":    join("terraform", "states", "remap", backend, []string{"--from", "a"}),
		"sem nenhuma": join("terraform", "states", "remap", backend),
		"iguais":      join("terraform", "states", "remap", backend, []string{"--from", "a", "--to", "A"}),
	}

	for name, args := range casos {
		t.Run(name, func(t *testing.T) {
			_, err := runCLI(t, "", args...)
			if err == nil {
				t.Fatal("erro = nil, quero falha")
			}
			if strings.Contains(err.Error(), "credencial") || strings.Contains(err.Error(), "DefaultAzureCredential") {
				t.Errorf("chegou a tentar autenticar antes de validar: %v", err)
			}
		})
	}
}

func TestRemapApareceNoGrupoStates(t *testing.T) {
	out, err := runCLI(t, "", "terraform", "states", "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(out, "remap") {
		t.Errorf("remap não aparece no grupo states:\n%s", out)
	}
}

func join(parts ...interface{}) []string {
	var out []string
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			out = append(out, v)
		case []string:
			out = append(out, v...)
		}
	}
	return out
}

// Sem o diretório do módulo o preflight não verifica a configuração, e é a
// configuração que decide se o 'import' depois do 'rm' acha o recurso. O
// --apply recusa antes de tocar em nada.
func TestRemapApplyExigeChdir(t *testing.T) {
	args := join("terraform", "states", "remap",
		[]string{"--account", "st", "--container", "c", "--key", "dev/app.tfstate"},
		[]string{"--from", "11111111-1111-1111-1111-111111111111",
			"--to", "22222222-2222-2222-2222-222222222222", "--apply"})

	_, err := runCLI(t, "", args...)
	if err == nil {
		t.Fatal("--apply sem --chdir passou")
	}
	if !strings.Contains(err.Error(), "--chdir") {
		t.Errorf("o erro não diz qual flag falta: %v", err)
	}
	if strings.Contains(err.Error(), "credencial") || strings.Contains(err.Error(), "DefaultAzureCredential") {
		t.Errorf("chegou a tentar autenticar: %v", err)
	}
}
