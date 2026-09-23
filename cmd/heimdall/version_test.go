package main

import (
	"strings"
	"testing"
)

// O -ldflags precisa vencer o BuildInfo: quem fixa a versão explicitamente
// está dizendo qual é.
func TestVersionDoLdflagsVence(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = "v9.9.9-fixada"

	if got := resolveVersion(); got != "v9.9.9-fixada" {
		t.Errorf("resolveVersion = %q, quero a fixada por ldflags", got)
	}
}

// Sem ldflags, a versão vem do BuildInfo. No binário de teste isso é o
// pseudo-version do repositório, então basta não ser um dos rótulos de
// ausência.
func TestVersionVemDoBuildInfo(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = ""
	got := resolveVersion()

	if got == "" {
		t.Fatal("resolveVersion = vazia")
	}
	if got == versionDevel {
		t.Errorf("resolveVersion = %q: o rótulo cru do Go não deveria vazar", got)
	}
	// O binário de teste é compilado dentro do repositório, então o Go carimba
	// um pseudo-version com o commit.
	if !strings.HasPrefix(got, "v") && got != versionDesenvolvimento && got != versionDesconhecida {
		t.Errorf("resolveVersion = %q, formato inesperado", got)
	}
}

// O --version precisa responder alguma coisa em qualquer forma de build.
func TestFlagVersion(t *testing.T) {
	out, err := runCLI(t, "", "--version")
	if err != nil {
		t.Fatalf("erro = %v", err)
	}

	if !strings.Contains(out, "heimdall version") {
		t.Errorf("saída = %q", out)
	}
	if strings.Contains(out, versionDevel) {
		t.Errorf("o rótulo cru do Go vazou para a saída: %q", out)
	}
}
