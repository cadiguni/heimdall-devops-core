package main

import (
	"github.com/spf13/cobra"

	azurex "github.com/cadiguni/heimdall-devops-core/internal/azure"
	pipelinedoctor "github.com/cadiguni/heimdall-devops-core/internal/pipeline"
)

type connectionsOptions struct {
	targetOptions
	output string
}

func newConnectionsCmd(gf *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "connections",
		Aliases: []string{"conn"},
		Short:   "Inspeciona as service connections do projeto",
		Long: `Consulta as service connections de um projeto do Azure DevOps e separa as que
dependem de segredo com validade das que não dependem de nada.

Somente leitura, e nenhum parâmetro de autorização é lido — só o esquema.`,
		// Sem NoArgs, um subcomando errado seria engolido como argumento e o
		// erro sairia sobre a flag seguinte, escondendo a causa.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newConnectionsListCmd(gf))

	return cmd
}

func newConnectionsListCmd(_ *globalFlags) *cobra.Command {
	opts := connectionsOptions{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista as service connections e o que vence",
		Long: `Lista as service connections do projeto, com o esquema de autenticação de cada
uma e se ela depende de segredo com validade.

A data de validade NÃO aparece, e não é omissão: a API do Azure DevOps não
expõe nenhuma. O segredo por trás da conexão vive no Entra ID (app
registration) ou no provedor externo. O que este comando responde é a pergunta
anterior, e mais útil na prática: quais conexões dependem de algo que vence, e
quais não dependem de nada.

Conexão do Azure por federação de identidade de carga de trabalho não tem
segredo e não vence. Migrar para ela resolve o problema em vez de adiá-lo.

Também aponta as conexões que o Azure DevOps não considera prontas, que
costumam ser credencial já inválida, e as suspeitas de duplicata: conexões do
mesmo tipo cujo nome traz o mesmo GUID de assinatura. É suspeita, não
veredito — pode haver motivo para as duas existirem. Conexão sem GUID no nome
não entra na comparação.

Exemplo:

  heimdall pipeline connections list --org minhaorg --project MeuProjeto`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConnectionsList(cmd, &opts)
		},
	}

	opts.bindDevOps(cmd)
	bindOutput(cmd, &opts.output)

	return cmd
}

func runConnectionsList(cmd *cobra.Command, opts *connectionsOptions) error {
	if err := validateOutput(opts.output); err != nil {
		return err
	}

	devops, err := opts.resolveDevOps()
	if err != nil {
		return err
	}

	client, err := azurex.NewDevOpsClient(devops.Org, devops.Project)
	if err != nil {
		return err
	}

	inventory, err := pipelinedoctor.ListConnections(cmd.Context(), client)
	if err != nil {
		return err
	}
	inventory.Project = client.ProjectURL()

	out := cmd.OutOrStdout()
	if opts.output == "json" {
		return encodeJSON(out, inventory)
	}
	return pipelinedoctor.WriteConnections(out, inventory)
}
