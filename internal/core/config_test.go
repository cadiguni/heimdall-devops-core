package core

import (
	"strings"
	"testing"
)

const arquivoDeExemplo = `
profiles:
  time1-dev:
    descricao: backend de dev do time 1
    account: stterraform
    container: time1
    key: dev/app.tfstate
  time1-prod:
    account: stterraform
    container: time1
    key: prod/app.tfstate
  devops:
    org: minhaorg
    project: MeuProjeto
`

func carregar(t *testing.T, texto string) *Config {
	t.Helper()

	cfg, err := ParseConfig(strings.NewReader(texto))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	return cfg
}

func TestParseConfig(t *testing.T) {
	cfg := carregar(t, arquivoDeExemplo)

	if len(cfg.Profiles) != 3 {
		t.Fatalf("perfis = %d, quero 3", len(cfg.Profiles))
	}

	dev, err := cfg.Profile("time1-dev")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if dev.Account != "stterraform" || dev.Container != "time1" || dev.Key != "dev/app.tfstate" {
		t.Errorf("perfil = %+v", dev)
	}

	devops, err := cfg.Profile("devops")
	if err != nil {
		t.Fatalf("Profile: %v", err)
	}
	if devops.Org != "minhaorg" || devops.Project != "MeuProjeto" {
		t.Errorf("perfil de devops = %+v", devops)
	}
}

// Campo desconhecido precisa virar erro: é o que pega um "acccount" digitado
// errado, e o que recusa alguém tentando guardar chave de acesso no arquivo.
func TestParseConfigRecusaCampoDesconhecido(t *testing.T) {
	casos := map[string]string{
		"nome errado":     "profiles:\n  p:\n    acccount: st\n",
		"chave de acesso": "profiles:\n  p:\n    account: st\n    access_key: segredo\n",
		"senha":           "profiles:\n  p:\n    password: segredo\n",
	}

	for name, texto := range casos {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseConfig(strings.NewReader(texto)); err == nil {
				t.Error("erro = nil, quero recusa do campo desconhecido")
			}
		})
	}
}

func TestParseConfigVazio(t *testing.T) {
	cfg := carregar(t, "")

	if cfg.Profiles == nil {
		t.Error("Profiles = nil, quero mapa vazio")
	}
	if len(cfg.ProfileNames()) != 0 {
		t.Errorf("nomes = %v", cfg.ProfileNames())
	}
}

func TestParseConfigInvalido(t *testing.T) {
	if _, err := ParseConfig(strings.NewReader("isso: [não fecha")); err == nil {
		t.Error("erro = nil, quero falha no YAML quebrado")
	}
}

// Errar o nome do perfil é engano comum; a mensagem resolve sozinha listando os
// disponíveis.
func TestProfileInexistenteListaOsDisponiveis(t *testing.T) {
	cfg := carregar(t, arquivoDeExemplo)

	_, err := cfg.Profile("time1-hml")
	if err == nil {
		t.Fatal("erro = nil, quero falha")
	}
	for _, esperado := range []string{"time1-hml", "time1-dev", "time1-prod", "devops"} {
		if !strings.Contains(err.Error(), esperado) {
			t.Errorf("mensagem não cita %q: %v", esperado, err)
		}
	}
}

func TestProfileEmArquivoSemPerfis(t *testing.T) {
	_, err := carregar(t, "").Profile("qualquer")
	if err == nil {
		t.Fatal("erro = nil, quero falha")
	}
	if !strings.Contains(err.Error(), "não tem nenhum") {
		t.Errorf("mensagem = %v", err)
	}
}

func TestProfileNamesEmOrdem(t *testing.T) {
	got := carregar(t, arquivoDeExemplo).ProfileNames()

	want := []string{"devops", "time1-dev", "time1-prod"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("nomes = %v, quero %v", got, want)
	}
}

// A flag sempre vence: é o que permite apontar um perfil e trocar só a chave.
func TestResolveBackendFlagVenceOPerfil(t *testing.T) {
	perfil := Profile{Account: "doPerfil", Container: "cPerfil", Key: "dev/app.tfstate"}

	got := ResolveBackend(Backend{Key: "hml/outro.tfstate"}, perfil)

	if got.Account != "doPerfil" || got.Container != "cPerfil" {
		t.Errorf("o que ficou em branco deveria vir do perfil: %+v", got)
	}
	if got.Key != "hml/outro.tfstate" {
		t.Errorf("Key = %q, a flag deveria vencer", got.Key)
	}
}

func TestResolveBackendSemPerfil(t *testing.T) {
	flags := Backend{Account: "st", Container: "c", Key: "k"}

	if got := ResolveBackend(flags, Profile{}); got != flags {
		t.Errorf("sem perfil o resultado deveria ser as flags: %+v", got)
	}
}

func TestResolveDevOps(t *testing.T) {
	perfil := Profile{Org: "orgPerfil", Project: "projPerfil"}

	got := ResolveDevOps(DevOps{Project: "outroProjeto"}, perfil)

	if got.Org != "orgPerfil" || got.Project != "outroProjeto" {
		t.Errorf("resolução = %+v", got)
	}
}

func TestBackendValidate(t *testing.T) {
	completo := Backend{Account: "st", Container: "c", Key: "k"}
	if err := completo.Validate(true); err != nil {
		t.Errorf("backend completo reprovou: %v", err)
	}

	// needKey falso é o caso do 'states list', que opera no container inteiro.
	semChave := Backend{Account: "st", Container: "c"}
	if err := semChave.Validate(false); err != nil {
		t.Errorf("sem chave e sem exigi-la deveria passar: %v", err)
	}
	if err := semChave.Validate(true); err == nil {
		t.Error("sem chave e exigindo-a deveria falhar")
	}

	err := Backend{}.Validate(true)
	if err == nil {
		t.Fatal("backend vazio deveria falhar")
	}
	// A mensagem cita as flags que faltam e lembra do perfil.
	for _, esperado := range []string{"--account", "--container", "--key", "--profile"} {
		if !strings.Contains(err.Error(), esperado) {
			t.Errorf("mensagem não cita %q: %v", esperado, err)
		}
	}
}

func TestDevOpsValidate(t *testing.T) {
	if err := (DevOps{Org: "o", Project: "p"}).Validate(); err != nil {
		t.Errorf("completo reprovou: %v", err)
	}

	err := DevOps{Org: "o"}.Validate()
	if err == nil {
		t.Fatal("sem projeto deveria falhar")
	}
	if !strings.Contains(err.Error(), "--project") || strings.Contains(err.Error(), "--org") {
		t.Errorf("mensagem deveria citar só o que falta: %v", err)
	}
}
