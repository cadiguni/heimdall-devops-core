package main

import (
	"fmt"

	"github.com/spf13/cobra"

	azurex "github.com/cadiguni/heimdall-devops-core/internal/azure"
	tfdoctor "github.com/cadiguni/heimdall-devops-core/internal/terraform"
)

type coverageOptions struct {
	targetOptions
	subscription   string
	prefix         string
	endpointSuffix string
	showTags       bool
	output         string
}

func newStatesCoverageCmd(_ *globalFlags) *cobra.Command {
	opts := coverageOptions{}

	cmd := &cobra.Command{
		Use:     "coverage",
		Aliases: []string{"cobertura"},
		Short:   "Diz quais resource groups da assinatura têm state e quais não têm",
		Long: `Cruza os resource groups de uma assinatura com o que os states de um container
rastreiam, e separa em três:

  - com state: existe na assinatura e algum state o rastreia, dizendo qual;
  - sem state neste container: existe e nenhum state rastreia — o candidato a
    ter sido feito à mão;
  - state rastreia resource group que não está na assinatura: foi apagado, ou
    movido para outra (o caso do 'states remap').

É a pergunta "o que aqui é Terraform e o que foi na mão?" respondida por
evidência, não por convenção de nome. O que decide é o id dos recursos dentro
do state: ele carrega a assinatura e o resource group.

Somente leitura, dos dois lados. Precisa de leitura na assinatura (plano de
controle) e no container (plano de dados) — são permissões diferentes, e o
comando diz qual faltou.

"Sem state neste container" é exatamente isso e não mais: um resource group
pode ter state em outro container ou em outra conta de armazenamento. O
relatório diz isso em vez de concluir por você.

Exemplos:

  heimdall terraform states coverage --subscription 00000000-0000-0000-0000-000000000001 \
    --account stterraform --container time1

  heimdall terraform states coverage --subscription ... --profile time1 --prefix dev/ --tags`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatesCoverage(cmd, &opts)
		},
	}

	opts.bindBackend(cmd)
	cmd.Flags().StringVar(&opts.subscription, "subscription", "", "assinatura a cruzar com o container")
	cmd.Flags().StringVar(&opts.prefix, "prefix", "", `limita os states a um caminho, ex.: "dev/"`)
	cmd.Flags().BoolVar(&opts.showTags, "tags", false,
		"mostra as tags dos resource groups sem state (é o que resta para saber de quem são)")
	bindEndpointSuffix(cmd, &opts.endpointSuffix)
	bindOutput(cmd, &opts.output)

	return cmd
}

func runStatesCoverage(cmd *cobra.Command, opts *coverageOptions) error {
	if err := validateOutput(opts.output); err != nil {
		return err
	}
	if opts.subscription == "" {
		return fmt.Errorf("informe a assinatura com --subscription")
	}

	// O prefixo é flag própria e não vem do perfil, como no 'states list': um
	// perfil aponta para o state do time, e herdar a chave dele reduziria a
	// varredura a um state só.
	client, err := containerClient(&opts.targetOptions, opts.endpointSuffix, opts.output, false)
	if err != nil {
		return err
	}

	inventory, err := tfdoctor.ListStates(cmd.Context(), client, tfdoctor.ListStatesOptions{
		Prefix: opts.prefix,
	})
	if err != nil {
		return err
	}

	caminhos := make([]string, 0, len(inventory.States))
	for _, s := range inventory.States {
		caminhos = append(caminhos, s.Path)
	}

	arm, err := azurex.NewARMClient(opts.subscription)
	if err != nil {
		return err
	}

	grupos, err := arm.ListResourceGroups(cmd.Context())
	if err != nil {
		return err
	}

	tracking := tfdoctor.CollectTracking(cmd.Context(), client, caminhos)

	report := tfdoctor.BuildCoverage(opts.subscription, grupos, tracking)
	report.Container = client.URL()

	out := cmd.OutOrStdout()
	if opts.output == "json" {
		return encodeJSON(out, report)
	}
	return tfdoctor.WriteCoverage(out, report, opts.showTags)
}
