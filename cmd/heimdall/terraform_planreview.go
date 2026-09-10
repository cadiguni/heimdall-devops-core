package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	tfdoctor "github.com/cadiguni/heimdall-devops-core/internal/terraform"
)

// exitCodeDestructive é o código de saída quando o plano contém destruição e o
// gate está ligado. Fica separado de 1 (erro de execução) para o pipeline poder
// distinguir "revisão reprovou" de "o heimdall quebrou".
const exitCodeDestructive = 2

type planReviewOptions struct {
	planJSON      string
	output        string
	failOnDestroy bool
}

func newPlanReviewCmd(_ *globalFlags) *cobra.Command {
	opts := planReviewOptions{}

	cmd := &cobra.Command{
		Use:   "plan-review",
		Short: "Revisa um plano do Terraform e destaca operações destrutivas",
		Long: `Analisa a saída de 'terraform show -json' e reporta o resumo das mudanças
e os recursos que serão destruídos, recriados ou removidos do state.

Somente leitura: não executa terraform, não toca no state e não imprime valores
de atributos (portanto não vaza secrets do plano).

Para gerar a entrada:

  terraform plan -out=tf.plan
  terraform show -json tf.plan > plan.json
  heimdall terraform plan-review --plan-json plan.json

Códigos de saída: 0 sem destruição, 2 destruição encontrada, 1 erro.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlanReview(cmd, &opts)
		},
	}

	cmd.Flags().StringVar(&opts.planJSON, "plan-json", "", `arquivo JSON de 'terraform show -json' ("-" lê do stdin)`)
	cmd.Flags().StringVarP(&opts.output, "output", "o", "text", "formato da saída: text ou json")
	cmd.Flags().BoolVar(&opts.failOnDestroy, "fail-on-destroy", true, "sair com código 2 quando houver operação destrutiva")
	cmd.MarkFlagRequired("plan-json")
	cmd.MarkFlagFilename("plan-json", "json")

	return cmd
}

func runPlanReview(cmd *cobra.Command, opts *planReviewOptions) error {
	if opts.output != "text" && opts.output != "json" {
		return fmt.Errorf("formato de saída inválido: %q (use text ou json)", opts.output)
	}

	in, closeIn, err := openPlanInput(cmd, opts.planJSON)
	if err != nil {
		return err
	}
	defer closeIn()

	plan, err := tfdoctor.ParsePlanJSON(in)
	if err != nil {
		return err
	}

	review := tfdoctor.ReviewPlan(plan)

	out := cmd.OutOrStdout()
	if opts.output == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(review); err != nil {
			return err
		}
	} else if err := tfdoctor.WriteTextReport(out, review); err != nil {
		return err
	}

	if opts.failOnDestroy && review.HasDestructive() {
		return &exitError{code: exitCodeDestructive}
	}
	return nil
}

// openPlanInput abre o arquivo do plano, ou o stdin quando o caminho é "-".
func openPlanInput(cmd *cobra.Command, path string) (io.Reader, func(), error) {
	if path == "-" {
		return cmd.InOrStdin(), func() {}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("não foi possível ler o plano: %w", err)
	}
	return f, func() { f.Close() }, nil
}
