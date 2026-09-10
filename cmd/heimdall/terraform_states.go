package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	azurex "github.com/cadiguni/heimdall-devops-core/internal/azure"
	tfdoctor "github.com/cadiguni/heimdall-devops-core/internal/terraform"
)

type statesListOptions struct {
	account        string
	container      string
	prefix         string
	endpointSuffix string
	includeAll     bool
	output         string
}

func newStatesCmd(gf *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "states",
		Short: "Inspeciona os arquivos de state em um container do Azure",
		Long: `Consulta os arquivos de state guardados em um container de Blob Storage.

Somente leitura: lista blobs e lê metadata, sem baixar nem escrever state, e
sem precisar de 'terraform init'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newStatesListCmd(gf))

	return cmd
}

func newStatesListCmd(_ *globalFlags) *cobra.Command {
	opts := statesListOptions{}

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista os states do container e o estado do lock de cada um",
		Long: `Lista os arquivos de state de um container, com data da última modificação,
tamanho e estado do lock.

O lock do backend azurerm é um lease no blob; quando há lock, a metadata do
blob diz qual operação o segurou, por quem e desde quando. É o suficiente para
responder "quem está travando esse state?" sem rodar 'terraform init'.

Autenticação pela identidade do ambiente (DefaultAzureCredential): localmente
basta um 'az login'. Não existe opção de access key: chave em flag vaza no
histórico do shell e no log da pipeline.

Exemplos:

  heimdall terraform states list --account stterraform --container time1
  heimdall terraform states list --account stterraform --container time1 --prefix prod/`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatesList(cmd, &opts)
		},
	}

	cmd.Flags().StringVar(&opts.account, "account", "", "nome da storage account")
	cmd.Flags().StringVar(&opts.container, "container", "", "nome do container")
	cmd.Flags().StringVar(&opts.prefix, "prefix", "", `limita a um caminho, ex.: "prod/"`)
	cmd.Flags().StringVar(&opts.endpointSuffix, "endpoint-suffix", azurex.DefaultEndpointSuffix, "sufixo do endpoint (clouds soberanas usam outro)")
	cmd.Flags().BoolVar(&opts.includeAll, "all", false, "lista também blobs que não parecem state")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "text", "formato da saída: text ou json")
	cmd.MarkFlagRequired("account")
	cmd.MarkFlagRequired("container")

	return cmd
}

func runStatesList(cmd *cobra.Command, opts *statesListOptions) error {
	if opts.output != "text" && opts.output != "json" {
		return fmt.Errorf("formato de saída inválido: %q (use text ou json)", opts.output)
	}

	client, err := azurex.NewContainerClient(opts.account, opts.container, opts.endpointSuffix)
	if err != nil {
		return err
	}

	inventory, err := tfdoctor.ListStates(cmd.Context(), client, tfdoctor.ListStatesOptions{
		Prefix:          opts.prefix,
		IncludeNonState: opts.includeAll,
	})
	if err != nil {
		return err
	}
	inventory.Container = client.URL()

	out := cmd.OutOrStdout()
	if opts.output == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(inventory)
	}

	return tfdoctor.WriteStatesReport(out, inventory, time.Now())
}
