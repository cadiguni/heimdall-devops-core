package main

import (
	"fmt"

	"github.com/spf13/cobra"

	azurex "github.com/cadiguni/heimdall-devops-core/internal/azure"
	tfdoctor "github.com/cadiguni/heimdall-devops-core/internal/terraform"
)

// exitCodeBlocked: o preflight reprovou o import. Separado do 1 de erro pelo
// mesmo motivo dos gates do plan-review.
const exitCodeBlocked = 2

type importOptions struct {
	targetOptions
	endpointSuffix string
	chdir          string
	apply          bool
	output         string
}

func newImportCmd(_ *globalFlags) *cobra.Command {
	opts := importOptions{}

	cmd := &cobra.Command{
		Use:   "import <endereço> <id-do-recurso>",
		Short: "Verifica e executa um import contra o state certo",
		Long: `Importa um recurso existente no Azure para o state do Terraform, depois de
verificar que a operação tem chance de dar certo e que é no state pretendido.

Antes de qualquer coisa, checa contra o state real:

  - o state existe no container (se não, a chave do backend está errada)
  - o state não está travado (se estiver, o import esperaria e estouraria)
  - o endereço ainda não está no state (se estiver, import é a operação errada)

Dry-run por padrão: mostra o alvo, o resultado das verificações e os comandos
equivalentes, sem executar nada. Só executa com --apply.

O 'terraform init' nunca é executado pelo heimdall, nem com --apply — o
diretório precisa já estar inicializado contra o backend certo. Reconfigurar o
backend de um diretório de trabalho alheio pode migrar state.

Exemplo:

  heimdall terraform import azurerm_resource_group.rg \
    /subscriptions/.../resourceGroups/app-dev-rg \
    --account stterraform --container time1 --key dev/app.tfstate`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(cmd, &opts, args[0], args[1])
		},
	}

	opts.bindBackend(cmd)
	opts.bindKey(cmd)
	bindEndpointSuffix(cmd, &opts.endpointSuffix)
	cmd.Flags().StringVar(&opts.chdir, "chdir", ".", "diretório do módulo Terraform, já inicializado")
	cmd.Flags().BoolVar(&opts.apply, "apply", false, "executa o import de verdade (sem isto é dry-run)")
	bindOutput(cmd, &opts.output)

	return cmd
}

func runImport(cmd *cobra.Command, opts *importOptions, address, resourceID string) error {
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

	preflight, err := tfdoctor.PreflightImport(cmd.Context(), client, tfdoctor.ImportRequest{
		Address:      address,
		ResourceID:   resourceID,
		Account:      backend.Account,
		Container:    backend.Container,
		Key:          backend.Key,
		ContainerURL: client.URL(),
	})
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

	if err := tfdoctor.WriteImportPreflight(out, preflight, opts.apply); err != nil {
		return err
	}

	if preflight.Blocked() {
		return &exitError{code: exitCodeBlocked}
	}
	if !opts.apply {
		return nil
	}

	importer, err := tfdoctor.NewExecImporter(opts.chdir, out, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	return importer.Import(cmd.Context(), address, resourceID)
}
