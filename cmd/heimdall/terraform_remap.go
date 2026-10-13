package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	azurex "github.com/cadiguni/heimdall-devops-core/internal/azure"
	tfdoctor "github.com/cadiguni/heimdall-devops-core/internal/terraform"
)

type remapOptions struct {
	targetOptions
	from           string
	to             string
	resourceGroup  string
	endpointSuffix string
	chdir          string
	backupDir      string
	apply          bool
	output         string
}

func newStatesRemapCmd(_ *globalFlags) *cobra.Command {
	opts := remapOptions{}

	cmd := &cobra.Command{
		Use:   "remap",
		Short: "Troca a assinatura nos ids de um state depois de um move",
		Long: `Depois de mover recursos de uma assinatura para outra, o state continua com os
ids antigos: o Terraform não acha os recursos, quer criar de novo, e o apply
falha com "a resource with the ID ... already exists".

Este comando acerta o state. Para cada recurso cujo id contém a assinatura de
origem, faz 'terraform state rm' seguido de 'terraform import' com o id novo —
nunca editando o JSON do state, conforme o princípio do projeto.

O id novo é derivado, não adivinhado: um move preserva o nome do resource group
e do recurso, então só o segmento da assinatura muda.

Os recursos do state caem em três grupos, e todos aparecem no dry-run:

  - os que serão remapeados;
  - os que mencionam a assinatura em formato não reescrevível (base64,
    querystring), que bloqueiam a execução e precisam de tratamento à mão;
  - os que não mencionam a assinatura e ficam como estão.

O MOVE NO PORTAL PRECISA TER ACONTECIDO ANTES. Quem confirma que o recurso
existe no destino é o próprio 'terraform import', que faz o read pelo provider —
o heimdall não checa isso no ARM, porque quem tem acesso só ao data plane do
Storage receberia "não existe" para recurso que existe.

Dry-run por padrão. Com --apply, o state é baixado para backup antes de
qualquer escrita, e a execução para na primeira falha dizendo onde parou.

Exemplo:

  heimdall terraform states remap \
    --from 00000000-0000-0000-0000-000000000001 \
    --to   00000000-0000-0000-0000-000000000002 \
    --rg app-dev-rg --profile time1-dev --chdir ../modulo`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatesRemap(cmd, &opts)
		},
	}

	opts.bindBackend(cmd)
	opts.bindKey(cmd)
	cmd.Flags().StringVar(&opts.from, "from", "", "assinatura de origem (a que está nos ids do state)")
	cmd.Flags().StringVar(&opts.to, "to", "", "assinatura de destino (para onde os recursos foram movidos)")
	cmd.Flags().StringVar(&opts.resourceGroup, "rg", "",
		"estreita a troca a um resource group (recomendado: o move costuma ser de um RG)")
	bindEndpointSuffix(cmd, &opts.endpointSuffix)
	cmd.Flags().StringVar(&opts.chdir, "chdir", ".", "diretório do módulo Terraform, já inicializado")
	cmd.Flags().StringVar(&opts.backupDir, "backup-dir", ".", "onde gravar o backup do state antes de escrever")
	cmd.Flags().BoolVar(&opts.apply, "apply", false, "executa de verdade (sem isto é dry-run)")
	bindOutput(cmd, &opts.output)

	return cmd
}

func runStatesRemap(cmd *cobra.Command, opts *remapOptions) error {
	if err := validateOutput(opts.output); err != nil {
		return err
	}
	if opts.output == "json" && opts.apply {
		return fmt.Errorf("--output json não combina com --apply: a saída do terraform não é JSON")
	}

	// As assinaturas são validadas antes de montar o client: a validação não
	// deve depender de a credencial existir.
	if err := tfdoctor.ValidateRemap(opts.from, opts.to); err != nil {
		return err
	}

	backend, err := opts.resolveBackend(true)
	if err != nil {
		return err
	}

	client, err := azurex.NewContainerClient(backend.Account, backend.Container, opts.endpointSuffix)
	if err != nil {
		return err
	}

	plan, err := tfdoctor.PlanRemap(cmd.Context(), client, tfdoctor.RemapRequest{
		From:          opts.from,
		To:            opts.to,
		ResourceGroup: opts.resourceGroup,
		Account:       backend.Account,
		Container:     backend.Container,
		Key:           backend.Key,
		ContainerURL:  client.URL(),
	})
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if opts.output == "json" {
		if err := encodeJSON(out, plan); err != nil {
			return err
		}
		if plan.Blocked() {
			return &exitError{code: exitCodeBlocked}
		}
		return nil
	}

	if err := tfdoctor.WriteRemapPlan(out, plan, opts.apply); err != nil {
		return err
	}

	if plan.Blocked() {
		return &exitError{code: exitCodeBlocked}
	}
	if !opts.apply {
		return nil
	}

	// O backup vem antes de qualquer escrita. Não é atômico lá na frente, então
	// o retrato de antes é a única forma de voltar se algo der muito errado.
	if err := backupState(cmd.Context(), client, backend.Key, opts.backupDir, out); err != nil {
		return err
	}

	runner, err := tfdoctor.NewExecImporter(opts.chdir, out, cmd.ErrOrStderr())
	if err != nil {
		return err
	}

	targets := plan.Targets()
	result, execErr := tfdoctor.ExecuteRemap(cmd.Context(), runner, targets, out)
	if err := tfdoctor.WriteRemapResult(out, result, targets); err != nil {
		return err
	}
	return execErr
}

// backupState baixa o state para um arquivo local antes de escrever.
func backupState(ctx context.Context, downloader azurex.BlobDownloader, key, dir string, out io.Writer) error {
	body, err := downloader.DownloadBlob(ctx, key)
	if err != nil {
		return fmt.Errorf("baixando backup do state: %w", err)
	}
	defer body.Close()

	nome := fmt.Sprintf("%s.%s.backup.tfstate",
		filepath.Base(key), time.Now().UTC().Format("20060102-150405"))
	caminho := filepath.Join(dir, nome)

	f, err := os.Create(caminho)
	if err != nil {
		return fmt.Errorf("criando backup: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, body); err != nil {
		return fmt.Errorf("gravando backup: %w", err)
	}

	_, err = fmt.Fprintf(out, "\nBackup do state antes de escrever: %s\n", caminho)
	return err
}
