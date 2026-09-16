package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	azurex "github.com/cadiguni/heimdall-devops-core/internal/azure"
	tfdoctor "github.com/cadiguni/heimdall-devops-core/internal/terraform"
)

type stateOpOptions struct {
	account        string
	container      string
	key            string
	endpointSuffix string
	chdir          string
	apply          bool
	output         string
}

func (o *stateOpOptions) bind(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.account, "account", "", "nome da storage account")
	cmd.Flags().StringVar(&o.container, "container", "", "nome do container")
	cmd.Flags().StringVar(&o.key, "key", "", "caminho do state dentro do container, ex.: dev/app.tfstate")
	cmd.Flags().StringVar(&o.endpointSuffix, "endpoint-suffix", azurex.DefaultEndpointSuffix, "sufixo do endpoint (clouds soberanas usam outro)")
	cmd.Flags().StringVar(&o.chdir, "chdir", ".", "diretório do módulo Terraform, já inicializado")
	cmd.Flags().BoolVar(&o.apply, "apply", false, "executa de verdade (sem isto é dry-run)")
	cmd.Flags().StringVarP(&o.output, "output", "o", "text", "formato da saída: text ou json")
	cmd.MarkFlagRequired("account")
	cmd.MarkFlagRequired("container")
	cmd.MarkFlagRequired("key")
}

func newStatesRmCmd(_ *globalFlags) *cobra.Command {
	opts := stateOpOptions{}

	cmd := &cobra.Command{
		Use:   "rm <endereço>",
		Short: "Tira um recurso do state sem destruir a infraestrutura",
		Long: `Remove um recurso do state. A infraestrutura no Azure continua existindo — só
deixa de ser rastreada pelo Terraform.

Antes de executar, verifica contra o state real que ele existe, que não está
travado e que o endereço realmente está lá. Também lista o que exatamente sai
do state: um endereço sem índice atinge todas as instâncias de um count ou
for_each, e é a diferença entre remover um recurso e remover dez sem perceber.

Dry-run por padrão. Só executa com --apply.

Exemplo:

  heimdall terraform states rm azurerm_resource_group.rg \
    --account stterraform --container time1 --key dev/app.tfstate`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStateOp(cmd, &opts, tfdoctor.StateOpRequest{
				Operation: tfdoctor.StateOpRemove,
				Address:   args[0],
			})
		},
	}

	opts.bind(cmd)
	return cmd
}

func newStatesMvCmd(_ *globalFlags) *cobra.Command {
	opts := stateOpOptions{}

	cmd := &cobra.Command{
		Use:   "mv <origem> <destino>",
		Short: "Troca o endereço de um recurso dentro do state",
		Long: `Move um recurso de um endereço para outro dentro do mesmo state, sem tocar na
infraestrutura. É o que se usa depois de renomear um recurso ou movê-lo para
dentro de um módulo.

Antes de executar, verifica que a origem está no state e que o destino não
está — mover para um endereço ocupado sobrescreveria o que estiver lá.

Dry-run por padrão. Só executa com --apply.

Exemplo:

  heimdall terraform states mv azurerm_storage_account.stg module.app.azurerm_storage_account.stg \
    --account stterraform --container time1 --key dev/app.tfstate`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStateOp(cmd, &opts, tfdoctor.StateOpRequest{
				Operation:   tfdoctor.StateOpMove,
				Address:     args[0],
				Destination: args[1],
			})
		},
	}

	opts.bind(cmd)
	return cmd
}

func runStateOp(cmd *cobra.Command, opts *stateOpOptions, req tfdoctor.StateOpRequest) error {
	if opts.output != "text" && opts.output != "json" {
		return fmt.Errorf("formato de saída inválido: %q (use text ou json)", opts.output)
	}
	if opts.output == "json" && opts.apply {
		return fmt.Errorf("--output json não combina com --apply: a saída do terraform não é JSON")
	}

	client, err := azurex.NewContainerClient(opts.account, opts.container, opts.endpointSuffix)
	if err != nil {
		return err
	}

	req.Account = opts.account
	req.Container = opts.container
	req.Key = opts.key
	req.ContainerURL = client.URL()

	preflight, err := tfdoctor.PreflightStateOp(cmd.Context(), client, req)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if opts.output == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(preflight); err != nil {
			return err
		}
		if preflight.Blocked() {
			return &exitError{code: exitCodeBlocked}
		}
		return nil
	}

	if err := tfdoctor.WriteStateOpPreflight(out, preflight, opts.apply); err != nil {
		return err
	}

	if preflight.Blocked() {
		return &exitError{code: exitCodeBlocked}
	}
	if !opts.apply {
		return nil
	}

	runner, err := tfdoctor.NewExecImporter(opts.chdir, out, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	if req.Operation == tfdoctor.StateOpMove {
		return runner.StateMv(cmd.Context(), req.Address, req.Destination)
	}
	return runner.StateRm(cmd.Context(), req.Address)
}
