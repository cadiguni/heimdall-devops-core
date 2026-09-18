package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// escreveConfig cria um arquivo de perfis temporário e devolve o caminho.
func escreveConfig(t *testing.T, conteudo string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), configFileName)
	if err := os.WriteFile(path, []byte(conteudo), 0o600); err != nil {
		t.Fatalf("escrevendo config: %v", err)
	}
	return path
}

const configDeTeste = `
profiles:
  time1-dev:
    account: stterraform
    container: time1
    key: dev/app.tfstate
  devops:
    org: minhaorg
    project: MeuProjeto
`

// Sem --profile o arquivo nem é procurado: quem passa tudo por flag não pode
// ser afetado por um arquivo quebrado no diretório.
func TestSemProfileNaoProcuraArquivo(t *testing.T) {
	opts := targetOptions{account: "st", container: "c", key: "k"}

	backend, err := opts.resolveBackend(true)
	if err != nil {
		t.Fatalf("resolveBackend: %v", err)
	}
	if backend.Account != "st" || backend.Key != "k" {
		t.Errorf("backend = %+v", backend)
	}
}

func TestProfilePreencheOQueFaltou(t *testing.T) {
	opts := targetOptions{profile: "time1-dev", configPath: escreveConfig(t, configDeTeste)}

	backend, err := opts.resolveBackend(true)
	if err != nil {
		t.Fatalf("resolveBackend: %v", err)
	}
	if backend.Account != "stterraform" || backend.Container != "time1" || backend.Key != "dev/app.tfstate" {
		t.Errorf("backend = %+v", backend)
	}
}

func TestFlagVenceOPerfil(t *testing.T) {
	opts := targetOptions{
		profile:    "time1-dev",
		configPath: escreveConfig(t, configDeTeste),
		key:        "hml/outro.tfstate",
	}

	backend, err := opts.resolveBackend(true)
	if err != nil {
		t.Fatalf("resolveBackend: %v", err)
	}
	if backend.Key != "hml/outro.tfstate" {
		t.Errorf("Key = %q, a flag deveria vencer", backend.Key)
	}
	if backend.Account != "stterraform" {
		t.Errorf("o resto deveria continuar vindo do perfil: %+v", backend)
	}
}

func TestPerfilDeDevOps(t *testing.T) {
	opts := targetOptions{profile: "devops", configPath: escreveConfig(t, configDeTeste)}

	devops, err := opts.resolveDevOps()
	if err != nil {
		t.Fatalf("resolveDevOps: %v", err)
	}
	if devops.Org != "minhaorg" || devops.Project != "MeuProjeto" {
		t.Errorf("devops = %+v", devops)
	}
}

func TestProfileInexistente(t *testing.T) {
	opts := targetOptions{profile: "nao-existe", configPath: escreveConfig(t, configDeTeste)}

	_, err := opts.resolveBackend(true)
	if err == nil {
		t.Fatal("erro = nil, quero falha")
	}
	if !strings.Contains(err.Error(), "time1-dev") {
		t.Errorf("mensagem deveria listar os disponíveis: %v", err)
	}
}

func TestProfileSemArquivo(t *testing.T) {
	opts := targetOptions{profile: "qualquer", configPath: filepath.Join(t.TempDir(), "nao-existe.yaml")}

	if _, err := opts.resolveBackend(true); err == nil {
		t.Error("erro = nil, quero falha ao abrir o arquivo")
	}
}

// O arquivo de perfis não pode guardar segredo: campo desconhecido é recusado.
func TestConfigComCampoDeSegredoEhRecusado(t *testing.T) {
	path := escreveConfig(t, "profiles:\n  p:\n    account: st\n    access_key: segredo\n")
	opts := targetOptions{profile: "p", configPath: path}

	_, err := opts.resolveBackend(false)
	if err == nil {
		t.Fatal("erro = nil, quero recusa")
	}
	// A mensagem cita o arquivo, para a pessoa saber onde corrigir.
	if !strings.Contains(err.Error(), configFileName) {
		t.Errorf("mensagem não cita o arquivo: %v", err)
	}
}

// Sem flag e sem perfil, a mensagem diz o que falta e lembra do --profile.
func TestSemAlvoNenhumAMensagemOrienta(t *testing.T) {
	_, err := runCLI(t, "", "terraform", "states", "list")

	if err == nil {
		t.Fatal("erro = nil, quero falha")
	}
	for _, esperado := range []string{"--account", "--container", "--profile"} {
		if !strings.Contains(err.Error(), esperado) {
			t.Errorf("mensagem não cita %q: %v", esperado, err)
		}
	}
	if got := exitCodeFor(err); got != 1 {
		t.Errorf("código de saída = %d, quero 1", got)
	}
}

// O list opera sobre o container inteiro, então não exige --key.
func TestListNaoExigeChave(t *testing.T) {
	opts := targetOptions{account: "st", container: "c"}

	if _, err := opts.resolveBackend(false); err != nil {
		t.Errorf("list não deveria exigir --key: %v", err)
	}
	if _, err := opts.resolveBackend(true); err == nil {
		t.Error("os comandos que mexem em um state precisam exigir --key")
	}
}
