package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	tfdoctor "github.com/cadiguni/heimdall-devops-core/internal/terraform"
)

// Códigos de saída do plan-review. Ficam separados de 1 (erro de execução) para
// o pipeline distinguir "a revisão reprovou" de "o heimdall quebrou", e
// separados entre si para distinguir os dois motivos de reprovação.
const (
	// exitCodeDestructive: o plano destrói, recria ou esquece algum recurso.
	exitCodeDestructive = 2

	// exitCodeIncomplete: o plano não cobre toda a configuração, então a
	// ausência de destruição não significa nada.
	exitCodeIncomplete = 3
)

type planReviewOptions struct {
	planJSON         string
	output           string
	failOnDestroy    bool
	failOnIncomplete bool
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

Códigos de saída:

  0  nada a apontar
  1  erro de execução
  2  operação destrutiva encontrada
  3  plano incompleto (uso de -target ou mudanças adiadas)

Um plano incompleto reprova por padrão porque a destruição pode estar
justamente no que ficou de fora dele. Planos gerados por Terraform anterior a
1.8 não informam se estão completos, e nesses casos o código 3 nunca dispara.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlanReview(cmd, &opts)
		},
	}

	cmd.Flags().StringVar(&opts.planJSON, "plan-json", "", `arquivo JSON de 'terraform show -json' ("-" lê do stdin)`)
	cmd.Flags().StringVarP(&opts.output, "output", "o", "text", "formato da saída: text ou json")
	cmd.Flags().BoolVar(&opts.failOnDestroy, "fail-on-destroy", true, "sair com código 2 quando houver operação destrutiva")
	cmd.Flags().BoolVar(&opts.failOnIncomplete, "fail-on-incomplete", true, "sair com código 3 quando o plano não cobrir toda a configuração")
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

	// Um pipe fechado trunca o relatório mas não invalida a revisão, então o
	// gate abaixo continua valendo: sob 'set -o pipefail' o código de saída
	// precisa refletir o plano, não o 'head' que saiu antes.
	if err := writeReview(cmd.OutOrStdout(), review, opts.output); err != nil && !isBrokenPipe(err) {
		return err
	}

	// Destrutiva tem precedência: quando as duas condições valem, o recurso que
	// vai ser destruído é o achado mais acionável, e o relatório já avisa que o
	// plano está incompleto.
	if opts.failOnDestroy && review.HasDestructive() {
		return &exitError{code: exitCodeDestructive}
	}
	if opts.failOnIncomplete && review.IsIncomplete() {
		// Em texto o relatório já traz o aviso, mas em JSON a única pista é o
		// campo "incomplete"; a mensagem no stderr explica o código 3 nos dois.
		return &exitError{
			code: exitCodeIncomplete,
			msg:  "plano incompleto: a revisão não cobre toda a configuração",
		}
	}
	return nil
}

func writeReview(out io.Writer, review *tfdoctor.PlanReview, format string) error {
	if format == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(review)
	}
	return tfdoctor.WriteTextReport(out, review)
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
