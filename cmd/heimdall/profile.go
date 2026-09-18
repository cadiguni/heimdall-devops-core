package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cadiguni/heimdall-devops-core/internal/core"
)

// configFileName é o nome procurado no diretório atual e no home.
const configFileName = ".heimdall.yaml"

// targetOptions são as flags de alvo, comuns aos comandos que falam com o
// Azure ou com o Azure DevOps.
//
// Nenhuma delas é marcada como obrigatória no cobra: a validação do cobra roda
// antes do RunE, ou seja antes de o perfil ter chance de preencher. Quem
// valida é o core, depois da resolução, com mensagem que cita a flag e lembra
// do --profile.
type targetOptions struct {
	profile    string
	configPath string

	account   string
	container string
	key       string

	org     string
	project string
}

func (o *targetOptions) bindProfile(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.profile, "profile", "", "perfil do arquivo de perfis")
	cmd.Flags().StringVar(&o.configPath, "config", "", "caminho do arquivo de perfis (padrão: .heimdall.yaml no diretório atual ou no home)")
}

func (o *targetOptions) bindBackend(cmd *cobra.Command) {
	o.bindProfile(cmd)
	cmd.Flags().StringVar(&o.account, "account", "", "nome da storage account")
	cmd.Flags().StringVar(&o.container, "container", "", "nome do container")
}

func (o *targetOptions) bindKey(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.key, "key", "", "caminho do state dentro do container, ex.: dev/app.tfstate")
}

func (o *targetOptions) bindDevOps(cmd *cobra.Command) {
	o.bindProfile(cmd)
	cmd.Flags().StringVar(&o.org, "org", "", "organização do Azure DevOps")
	cmd.Flags().StringVar(&o.project, "project", "", "projeto do Azure DevOps")
}

// resolveBackend junta flags e perfil e valida o resultado.
func (o *targetOptions) resolveBackend(needKey bool) (core.Backend, error) {
	profile, err := o.loadProfile()
	if err != nil {
		return core.Backend{}, err
	}

	backend := core.ResolveBackend(core.Backend{
		Account:   o.account,
		Container: o.container,
		Key:       o.key,
	}, profile)

	if err := backend.Validate(needKey); err != nil {
		return core.Backend{}, err
	}
	return backend, nil
}

// resolveDevOps junta flags e perfil para os comandos de Azure DevOps.
func (o *targetOptions) resolveDevOps() (core.DevOps, error) {
	profile, err := o.loadProfile()
	if err != nil {
		return core.DevOps{}, err
	}

	devops := core.ResolveDevOps(core.DevOps{Org: o.org, Project: o.project}, profile)
	if err := devops.Validate(); err != nil {
		return core.DevOps{}, err
	}
	return devops, nil
}

// loadProfile devolve o perfil pedido, ou um vazio quando não se pediu nenhum.
func (o *targetOptions) loadProfile() (core.Profile, error) {
	if o.profile == "" {
		// Sem --profile o arquivo nem é procurado: quem passa tudo por flag não
		// deve ser afetado por um arquivo de perfis quebrado no diretório.
		return core.Profile{}, nil
	}

	path, err := o.findConfig()
	if err != nil {
		return core.Profile{}, err
	}

	f, err := os.Open(path)
	if err != nil {
		return core.Profile{}, fmt.Errorf("não foi possível ler o arquivo de perfis: %w", err)
	}
	defer f.Close()

	cfg, err := core.ParseConfig(f)
	if err != nil {
		return core.Profile{}, fmt.Errorf("%s: %w", path, err)
	}

	return cfg.Profile(o.profile)
}

// findConfig procura o arquivo de perfis: o caminho explícito, senão o
// diretório atual, senão o home.
func (o *targetOptions) findConfig() (string, error) {
	if o.configPath != "" {
		return o.configPath, nil
	}

	candidatos := []string{configFileName}
	if home, err := os.UserHomeDir(); err == nil {
		candidatos = append(candidatos, filepath.Join(home, configFileName))
	}

	for _, c := range candidatos {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}

	return "", fmt.Errorf("--profile %q pedido, mas não achei %s no diretório atual nem no home",
		o.profile, configFileName)
}
