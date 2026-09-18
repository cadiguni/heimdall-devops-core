package main

import (
	"fmt"

	"github.com/spf13/cobra"

	azurex "github.com/cadiguni/heimdall-devops-core/internal/azure"
	tfdoctor "github.com/cadiguni/heimdall-devops-core/internal/terraform"
)

type stateOpOptions struct {
	targetOptions
	endpointSuffix string
	chdir          string
	apply          bool
	output         string
}

func (o *stateOpOptions) bind(cmd *cobra.Command) {
	o.bindBackend(cmd)
	o.bindKey(cmd)
	bindEndpointSuffix(cmd, &o.endpointSuffix)
	cmd.Flags().StringVar(&o.chdir, "chdir", ".", "diretório do módulo Terraform, já inicializado")
	cmd.Flags().BoolVar(&o.apply, "apply", false, "executa de verdade (sem isto é dry-run)")
	bindOutput(cmd, &o.output)
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
	if err := validateOutput(opts.output); err != nil {
		return err
	}
	if opts.output == "json" && opts.apply {
		return fmt.Errorf("--output json não combina com --apply: a saída do terraform não é JSON")
	}

	backend, err := opts.resolveBackend(true)
	if err != nil {
		return err
	}

	client, err := azurex.NewContainerClient(backend.Account, backend.Container, opts.endpointSuffix)
	if err != nil {
		return err
	}

	req.Account = backend.Account
	req.Container = backend.Container
	req.Key = backend.Key
	req.ContainerURL = client.URL()

	preflight, err := tfdoctor.PreflightStateOp(cmd.Context(), client, req)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if opts.output == "json" {
		if err := encodeJSON(out, preflight); err != nil {
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
