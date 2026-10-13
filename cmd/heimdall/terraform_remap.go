package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	cmd.Flags().StringVar(&opts.chdir, "chdir", "",
		"diretório do módulo Terraform, já inicializado (exigido por --apply)")
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

	// Sem o diretório do módulo não dá para verificar que o import vai achar a
	// configuração, e é o import que, falhando depois do rm, deixa o recurso
	// fora do state. Então o --apply exige o --chdir.
	if opts.apply && opts.chdir == "" {
		return tfdoctor.ErrModuleRequired()
	}

	backend, err := opts.resolveBackend(true)
	if err != nil {
		return err
	}

	client, err := azurex.NewContainerClient(backend.Account, backend.Container, opts.endpointSuffix)
	if err != nil {
		return err
	}

	var snap *tfdoctor.ModuleSnapshot
	if opts.chdir != "" {
		snap = moduleSnapshot(cmd.Context(), opts.chdir)
	}

	plan, err := tfdoctor.PlanRemap(cmd.Context(), client, tfdoctor.RemapRequest{
		From:          opts.from,
		To:            opts.to,
		ResourceGroup: opts.resourceGroup,
		Account:       backend.Account,
		Container:     backend.Container,
		Key:           backend.Key,
		ContainerURL:  client.URL(),
		Module:        snap,
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

// moduleSnapshot lê o diretório do módulo para o preflight: os arquivos .tf e o
// state que o diretório usa de verdade.
//
// Nenhum erro aqui interrompe o comando. Um diretório que não dá para
// inspecionar é motivo de o preflight reprovar, com a causa escrita — e não de
// o heimdall quebrar antes de mostrar o plano.
func moduleSnapshot(ctx context.Context, dir string) *tfdoctor.ModuleSnapshot {
	snap := &tfdoctor.ModuleSnapshot{Dir: dir}

	arquivos, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		snap.FilesErr = err.Error()
	}
	for _, caminho := range arquivos {
		conteudo, err := os.ReadFile(caminho)
		if err != nil {
			snap.FilesErr = err.Error()
			break
		}
		snap.Files = append(snap.Files, tfdoctor.ModuleFile{
			Name:    filepath.Base(caminho),
			Content: string(conteudo),
		})
	}

	// Configuração em .tf.json existe e não é interpretada aqui. A contagem vai
	// para o domínio avisar em vez de afirmar que falta recurso.
	if json, err := filepath.Glob(filepath.Join(dir, "*.tf.json")); err == nil {
		snap.JSONFileCount = len(json)
	}

	// O state vem com todos os atributos dentro, inclusive segredo. Vai para
	// io.Discard de propósito: quem lê é só o parser, que fica com o id.
	var stderr bytes.Buffer
	puller, err := tfdoctor.NewExecImporter(dir, io.Discard, &stderr)
	if err != nil {
		snap.StatePullErr = err.Error()
		return snap
	}

	raw, err := puller.StatePull(ctx)
	if err != nil {
		snap.StatePullErr = resumirErro(err, &stderr)
		return snap
	}
	snap.RawState = raw

	return snap
}

// resumirErro junta o erro do terraform com a última linha útil do stderr, que
// é onde mora a causa de verdade ("Backend initialization required", por
// exemplo).
func resumirErro(err error, stderr *bytes.Buffer) string {
	msg := err.Error()

	var ultima string
	for _, linha := range strings.Split(stderr.String(), "\n") {
		if l := strings.TrimSpace(linha); l != "" {
			ultima = l
		}
	}
	if ultima != "" && !strings.Contains(msg, ultima) {
		msg += ": " + ultima
	}
	return msg
}
