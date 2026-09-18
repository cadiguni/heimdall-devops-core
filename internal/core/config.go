// Package core contém a lógica de negócio compartilhada entre os módulos,
// sem I/O direto (sem prints, sem exec, sem chamadas de rede), para poder ser
// reaproveitada tanto pela CLI quanto por um futuro cmd/heimdall-server.
package core

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Profile guarda os apontamentos repetidos em todo comando: onde fica o state
// no Azure, e qual organização e projeto do Azure DevOps.
//
// Nada aqui é segredo — nome de conta, container, caminho e projeto. O arquivo
// de perfis é feito para ser versionado junto do repositório, e a decodificação
// é estrita justamente para recusar qualquer campo inventado, inclusive uma
// tentativa de colocar chave de acesso dentro dele.
type Profile struct {
	// Descricao é livre, só para quem lê o arquivo.
	Descricao string `yaml:"descricao"`

	Account   string `yaml:"account"`
	Container string `yaml:"container"`
	Key       string `yaml:"key"`

	Org     string `yaml:"org"`
	Project string `yaml:"project"`
}

// Config é o arquivo de perfis.
type Config struct {
	Profiles map[string]Profile `yaml:"profiles"`
}

// ParseConfig lê o arquivo de perfis.
//
// A decodificação é estrita: campo desconhecido vira erro em vez de ser
// ignorado em silêncio. Assim um "acccount" digitado errado aparece na hora, e
// um "access_key" colocado ali por engano é recusado em vez de guardado.
func ParseConfig(r io.Reader) (*Config, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		if err == io.EOF {
			return &Config{Profiles: map[string]Profile{}}, nil
		}
		return nil, fmt.Errorf("arquivo de perfis inválido: %w", err)
	}

	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	return &cfg, nil
}

// Profile devolve o perfil pelo nome.
//
// Quando não existe, o erro lista os disponíveis: errar o nome do perfil é o
// tipo de engano que a mensagem resolve sozinha.
func (c *Config) Profile(name string) (Profile, error) {
	p, ok := c.Profiles[name]
	if ok {
		return p, nil
	}

	if len(c.Profiles) == 0 {
		return Profile{}, fmt.Errorf("perfil %q não existe: o arquivo de perfis não tem nenhum", name)
	}
	return Profile{}, fmt.Errorf("perfil %q não existe. Disponíveis: %s",
		name, strings.Join(c.ProfileNames(), ", "))
}

// ProfileNames lista os nomes em ordem estável.
func (c *Config) ProfileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Backend é o alvo de um comando de Terraform, depois de resolvido.
type Backend struct {
	Account   string
	Container string
	Key       string
}

// DevOps é o alvo de um comando de Azure DevOps, depois de resolvido.
type DevOps struct {
	Org     string
	Project string
}

// ResolveBackend combina o que veio por flag com o que veio do perfil.
//
// A flag sempre vence: o perfil preenche só o que ficou em branco. É o que
// permite apontar um perfil e trocar só a chave do state na linha de comando.
func ResolveBackend(flags Backend, profile Profile) Backend {
	return Backend{
		Account:   firstNonEmpty(flags.Account, profile.Account),
		Container: firstNonEmpty(flags.Container, profile.Container),
		Key:       firstNonEmpty(flags.Key, profile.Key),
	}
}

// ResolveDevOps combina flags e perfil para os comandos de Azure DevOps.
func ResolveDevOps(flags DevOps, profile Profile) DevOps {
	return DevOps{
		Org:     firstNonEmpty(flags.Org, profile.Org),
		Project: firstNonEmpty(flags.Project, profile.Project),
	}
}

// Validate confere o que o comando precisa, citando a flag e o perfil.
//
// needKey é falso nos comandos que operam sobre o container inteiro, como o
// 'states list'.
func (b Backend) Validate(needKey bool) error {
	faltando := []string{}
	if b.Account == "" {
		faltando = append(faltando, "--account")
	}
	if b.Container == "" {
		faltando = append(faltando, "--container")
	}
	if needKey && b.Key == "" {
		faltando = append(faltando, "--key")
	}
	return missingError(faltando)
}

// Validate confere organização e projeto.
func (d DevOps) Validate() error {
	faltando := []string{}
	if d.Org == "" {
		faltando = append(faltando, "--org")
	}
	if d.Project == "" {
		faltando = append(faltando, "--project")
	}
	return missingError(faltando)
}

func missingError(faltando []string) error {
	if len(faltando) == 0 {
		return nil
	}
	return fmt.Errorf("faltou %s — informe na linha de comando ou em um perfil (--profile)",
		strings.Join(faltando, " e "))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
