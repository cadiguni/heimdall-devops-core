package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	azurex "github.com/cadiguni/heimdall-devops-core/internal/azure"
	pipelinedoctor "github.com/cadiguni/heimdall-devops-core/internal/pipeline"
)

// extensoesVarridas são os arquivos em que uma assinatura aparece na prática:
// configuração do Terraform, variáveis, e YAML de pipeline.
var extensoesVarridas = []string{".tf", ".tfvars", ".yml", ".yaml", ".json", ".tfvars.json"}

type referencesOptions struct {
	targetOptions
	subscription string
	names        []string
	chdir        string
	skipDevOps   bool
	output       string
}

func newReferencesCmd(_ *globalFlags) *cobra.Command {
	opts := referencesOptions{}

	cmd := &cobra.Command{
		Use:   "references",
		Short: "Mostra onde uma assinatura ainda é citada",
		Long: `Depois de mover recursos de uma assinatura para outra, o tfstate é só uma das
coisas que continuam apontando para a antiga. Este comando procura as outras.

Varre três lugares:

  - as service connections do projeto, pelo campo data.subscriptionId;
  - as variáveis dos Variable Groups — a library;
  - os arquivos de um diretório do repositório (.tf, .tfvars, .yml, .yaml,
    .json), com arquivo e linha.

Somente leitura. O heimdall não altera Variable Group nem service connection:
ele diz onde está, e a alteração é de quem conhece o efeito dela.

O valor dos achados não é impresso. Valor de variável pode ser connection
string com a assinatura no meio, e a saída de um comando acaba em log de
pipeline — o que sai é o endereço do que precisa mudar.

Variável secreta aparece em seção própria, como não verificável: a API do Azure
DevOps devolve null no valor dessas, então não há como saber se citam a
assinatura. Contar como limpa seria afirmar o que não se sabe.

Exemplos:

  heimdall pipeline references --subscription 00000000-0000-0000-0000-000000000001 \
    --org minhaorg --project MeuProjeto --chdir ../modulo

  heimdall pipeline references --subscription 00000000-0000-0000-0000-000000000001 \
    --chdir . --skip-devops`,
		Aliases: []string{"refs"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReferences(cmd, &opts)
		},
	}

	opts.bindDevOps(cmd)
	cmd.Flags().StringVar(&opts.subscription, "subscription", "",
		"assinatura procurada (a antiga, a que saiu)")
	cmd.Flags().StringSliceVar(&opts.names, "also", nil,
		"outros textos a procurar, como nome de service connection ou da assinatura")
	cmd.Flags().StringVar(&opts.chdir, "chdir", "",
		"diretório do repositório a varrer (sem isto, só a organização é varrida)")
	cmd.Flags().BoolVar(&opts.skipDevOps, "skip-devops", false,
		"não consulta o Azure DevOps; varre só o repositório")
	bindOutput(cmd, &opts.output)

	return cmd
}

func runReferences(cmd *cobra.Command, opts *referencesOptions) error {
	if err := validateOutput(opts.output); err != nil {
		return err
	}
	if opts.subscription == "" && len(opts.names) == 0 {
		return fmt.Errorf("informe o que procurar: --subscription com a assinatura antiga, ou --also")
	}
	if opts.skipDevOps && opts.chdir == "" {
		return fmt.Errorf("--skip-devops sem --chdir não varreria nada")
	}

	req := pipelinedoctor.ReferenceRequest{
		Subscription: opts.subscription,
		Names:        opts.names,
	}

	report := &pipelinedoctor.ReferenceReport{}

	if !opts.skipDevOps {
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
		req.Connections = inventory.Connections

		// Os valores são lidos para serem procurados, nunca impressos: o
		// renderizador de referências não tem caminho que imprima valor.
		groups, err := pipelinedoctor.AllVariableGroupDetails(cmd.Context(), client)
		if err != nil {
			return err
		}
		req.Groups = groups

		report.Project = client.ProjectURL()
	}

	if opts.chdir != "" {
		arquivos, notas, err := lerRepositorio(opts.chdir)
		if err != nil {
			return err
		}
		req.Files = arquivos
		report.RepoDir = opts.chdir
		report.Notes = notas
	}

	resultado := pipelinedoctor.FindReferences(req)
	resultado.Project = report.Project
	resultado.RepoDir = report.RepoDir
	resultado.Notes = append(report.Notes, resultado.Notes...)

	out := cmd.OutOrStdout()
	if opts.output == "json" {
		return encodeJSON(out, resultado)
	}
	return pipelinedoctor.WriteReferences(out, resultado)
}

// lerRepositorio lê os arquivos varridos de um diretório, recursivamente.
//
// Pula .git, .terraform e node_modules: são cópias e cache, e um achado dentro
// deles não é lugar de alterar nada. As notas registram o que foi pulado e por
// quê, porque uma varredura que esconde o próprio limite engana.
func lerRepositorio(dir string) ([]pipelinedoctor.RepoFile, []string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("lendo %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s não é um diretório", dir)
	}

	const limiteBytes = 2 << 20 // 2 MiB

	var arquivos []pipelinedoctor.RepoFile
	var notas []string
	pulados := map[string]int{}

	err = filepath.WalkDir(dir, func(caminho string, d os.DirEntry, err error) error {
		if err != nil {
			notas = append(notas, fmt.Sprintf("%s não foi lido: %v", caminho, err))
			return nil
		}

		if d.IsDir() {
			switch d.Name() {
			case ".git", ".terraform", "node_modules":
				pulados[d.Name()]++
				return filepath.SkipDir
			}
			return nil
		}

		if !temExtensaoVarrida(d.Name()) {
			return nil
		}

		if fi, err := d.Info(); err == nil && fi.Size() > limiteBytes {
			notas = append(notas, fmt.Sprintf("%s foi pulado por tamanho (%d bytes)",
				relativo(dir, caminho), fi.Size()))
			return nil
		}

		conteudo, err := os.ReadFile(caminho)
		if err != nil {
			notas = append(notas, fmt.Sprintf("%s não foi lido: %v", relativo(dir, caminho), err))
			return nil
		}

		arquivos = append(arquivos, pipelinedoctor.RepoFile{
			Path:    relativo(dir, caminho),
			Content: string(conteudo),
		})
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("varrendo %s: %w", dir, err)
	}

	for nome, quantas := range pulados {
		notas = append(notas, fmt.Sprintf("%d diretório(s) %s foram pulados (cópia ou cache)", quantas, nome))
	}

	return arquivos, notas, nil
}

func temExtensaoVarrida(nome string) bool {
	baixo := strings.ToLower(nome)
	for _, ext := range extensoesVarridas {
		if strings.HasSuffix(baixo, ext) {
			return true
		}
	}
	return false
}

func relativo(base, caminho string) string {
	rel, err := filepath.Rel(base, caminho)
	if err != nil {
		return caminho
	}
	return filepath.ToSlash(rel)
}
