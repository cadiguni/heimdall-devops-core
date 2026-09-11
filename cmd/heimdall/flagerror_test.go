package main

import (
	"strings"
	"testing"
)

// Um binário desatualizado não tem o subcomando que a pessoa espera. O cobra
// faz o parse das flags antes de validar argumentos, então sem tratamento o
// erro sai sobre a primeira flag e esconde a causa — foi exatamente assim que
// um 'states show' rodado num binário antigo virou "unknown flag: --account".
func TestSubcomandoErradoComFlagApontaACausa(t *testing.T) {
	out, err := runCLI(t, "", "terraform", "states", "inexistente", "--account", "x", "--container", "y")

	if err == nil {
		t.Fatal("erro = nil, quero falha")
	}

	msg := err.Error()
	if !strings.Contains(msg, `subcomando desconhecido "inexistente"`) {
		t.Errorf("mensagem não aponta o subcomando: %q", msg)
	}
	if strings.Contains(msg, "--account") {
		t.Errorf("mensagem ainda culpa a flag: %q", msg)
	}
	// Listar o que existe é o que resolve o caso do binário desatualizado.
	if !strings.Contains(msg, "list") || !strings.Contains(msg, "show") {
		t.Errorf("mensagem não lista os subcomandos disponíveis: %q", msg)
	}

	if got := exitCodeFor(err); got != 1 {
		t.Errorf("código de saída = %d, quero 1", got)
	}
	_ = out
}

// Sem flag junto, o cobra já dá uma mensagem boa: não substituir.
func TestSubcomandoErradoSemFlag(t *testing.T) {
	_, err := runCLI(t, "", "terraform", "states", "inexistente")

	if err == nil {
		t.Fatal("erro = nil, quero falha")
	}
	if !strings.Contains(err.Error(), "inexistente") {
		t.Errorf("mensagem = %q", err.Error())
	}
}

// Flag desconhecida de verdade continua reportada como flag desconhecida.
func TestFlagDesconhecidaContinuaSendoFlag(t *testing.T) {
	casos := map[string][]string{
		"em comando-folha":            {"terraform", "states", "list", "--naoexiste"},
		"em grupo, sem subcomando":    {"terraform", "--naoexiste"},
		"em grupo com flag só depois": {"pipeline", "--naoexiste"},
	}

	for name, args := range casos {
		t.Run(name, func(t *testing.T) {
			_, err := runCLI(t, "", args...)
			if err == nil {
				t.Fatal("erro = nil, quero falha")
			}
			if !strings.Contains(err.Error(), "unknown flag") {
				t.Errorf("mensagem = %q, esperava erro de flag", err.Error())
			}
		})
	}
}

// Os comandos-grupo não podem aceitar argumento posicional em silêncio: era
// isso que engolia o subcomando errado.
func TestGruposRecusamArgumentoPosicional(t *testing.T) {
	grupos := [][]string{
		{"terraform", "sobrando"},
		{"terraform", "states", "sobrando"},
		{"pipeline", "sobrando"},
	}

	for _, args := range grupos {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := runCLI(t, "", args...); err == nil {
				t.Error("erro = nil, quero falha")
			}
		})
	}
}

// E sem argumento nenhum continuam mostrando o help.
func TestGruposMostramHelpSemArgs(t *testing.T) {
	grupos := [][]string{
		{"terraform"},
		{"terraform", "states"},
		{"pipeline"},
	}

	for _, args := range grupos {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, err := runCLI(t, "", args...)
			if err != nil {
				t.Fatalf("erro = %v", err)
			}
			if !strings.Contains(out, "Usage:") {
				t.Errorf("não mostrou o help:\n%s", out)
			}
		})
	}
}
