package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	azurex "github.com/cadiguni/heimdall-devops-core/internal/azure"
	pipelinedoctor "github.com/cadiguni/heimdall-devops-core/internal/pipeline"
)

type variablesOptions struct {
	organization string
	project      string
	showValues   bool
	output       string
}

func (o *variablesOptions) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.organization, "org", "", "organização do Azure DevOps")
	cmd.Flags().StringVar(&o.project, "project", "", "projeto do Azure DevOps")
	cmd.Flags().StringVarP(&o.output, "output", "o", "text", "formato da saída: text ou json")
	cmd.MarkFlagRequired("org")
	cmd.MarkFlagRequired("project")
}

func newVariablesCmd(gf *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "variables",
		Aliases: []string{"vars"},
		Short:   "Inspeciona Variable Groups do Azure DevOps",
		Long: `Consulta os Variable Groups de um projeto do Azure DevOps.

Somente leitura. Autenticação pela identidade do ambiente, a mesma dos comandos
de Terraform: localmente basta um 'az login'. Não existe opção de PAT — um PAT
é segredo de longa duração e vazaria em histórico de shell e em log.`,
		// Sem NoArgs, um subcomando errado seria engolido como argumento e o
		// erro sairia sobre a flag seguinte, escondendo a causa.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newVariablesListCmd(gf))
	cmd.AddCommand(newVariablesShowCmd(gf))

	return cmd
}

func newVariablesListCmd(_ *globalFlags) *cobra.Command {
	opts := variablesOptions{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista os Variable Groups do projeto",
		Long: `Lista os Variable Groups de um projeto, com quantas variáveis cada um tem e
quantas dessas são secretas.

Nenhum valor de variável é lido aqui — só as contagens.

Exemplo:

  heimdall pipeline variables list --org minhaorg --project MeuProjeto`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVariablesList(cmd, &opts)
		},
	}

	opts.bind(cmd)
	return cmd
}

func newVariablesShowCmd(_ *globalFlags) *cobra.Command {
	opts := variablesOptions{}

	cmd := &cobra.Command{
		Use:   "show <nome-ou-id>",
		Short: "Lista as variáveis de um Variable Group",
		Long: `Mostra as variáveis de um Variable Group, marcando quais são secretas.

Por padrão sai só o nome de cada variável. Um Variable Group é um repositório de
segredos, e variável não marcada como secreta frequentemente guarda coisa que
deveria ser — então revelar valor exige --show-values.

Mesmo com --show-values, valor de variável secreta não aparece: a API do Azure
DevOps devolve null para essas, então não há o que revelar.

Exemplos:

  heimdall pipeline variables show terraform-dev --org minhaorg --project MeuProjeto
  heimdall pipeline variables show 42 --org minhaorg --project MeuProjeto --show-values`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVariablesShow(cmd, &opts, args[0])
		},
	}

	opts.bind(cmd)
	cmd.Flags().BoolVar(&opts.showValues, "show-values", false,
		"revela o valor das variáveis não secretas (não use em log de pipeline)")

	return cmd
}

func runVariablesList(cmd *cobra.Command, opts *variablesOptions) error {
	client, err := devOpsClient(opts)
	if err != nil {
		return err
	}

	inventory, err := pipelinedoctor.ListVariableGroups(cmd.Context(), client)
	if err != nil {
		return err
	}
	inventory.Project = client.ProjectURL()

	out := cmd.OutOrStdout()
	if opts.output == "json" {
		return encodeJSON(out, inventory)
	}
	return pipelinedoctor.WriteVariableGroups(out, inventory)
}

func runVariablesShow(cmd *cobra.Command, opts *variablesOptions, nameOrID string) error {
	client, err := devOpsClient(opts)
	if err != nil {
		return err
	}

	detail, err := pipelinedoctor.FindVariableGroup(cmd.Context(), client, nameOrID, opts.showValues)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if opts.output == "json" {
		return encodeJSON(out, detail)
	}

	// O projeto aparece antes do conteúdo, como o container nos comandos de
	// state: o mesmo nome de grupo existe em projetos diferentes.
	if _, err := fmt.Fprintf(out, "Projeto: %s\n\n", client.ProjectURL()); err != nil {
		return err
	}
	return pipelinedoctor.WriteVariableGroupDetail(out, detail)
}

func devOpsClient(opts *variablesOptions) (*azurex.DevOpsClient, error) {
	if opts.output != "text" && opts.output != "json" {
		return nil, fmt.Errorf("formato de saída inválido: %q (use text ou json)", opts.output)
	}
	return azurex.NewDevOpsClient(opts.organization, opts.project)
}

func encodeJSON(out interface{ Write([]byte) (int, error) }, v interface{}) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
