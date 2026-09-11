package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	pipelinedoctor "github.com/cadiguni/heimdall-devops-core/internal/pipeline"
)

func newPipelineCmd(gf *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "pipeline",
		Aliases: []string{"pipe"},
		Short:   "Diagnóstico de pipelines (Azure DevOps)",
		Long: `Módulo Pipeline Doctor.

Subcomando disponível:

  diagnose  lê um log de pipeline e aponta as falhas que reconhece`,
		// Sem NoArgs, um subcomando errado seria engolido como argumento e o
		// erro sairia sobre a flag seguinte, escondendo a causa.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newDiagnoseCmd(gf))

	return cmd
}

type diagnoseOptions struct {
	logPath string
	output  string
}

func newDiagnoseCmd(_ *globalFlags) *cobra.Command {
	opts := diagnoseOptions{}

	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "Lê um log de pipeline e aponta as falhas conhecidas",
		Long: `Varre um log de pipeline e reconhece falhas conhecidas, dizendo em cada caso
qual costuma ser a causa e o que fazer. Quando o log traz dado suficiente,
monta o comando de correção — por exemplo o 'terraform import' com endereço e
ID já preenchidos.

O catálogo de assinaturas veio de falhas reais. Erros que são só consequência
("TerraformPlanFailed", "failed with exit code 1") ficam de fora de propósito:
aparecem em toda falha e não apontam para causa nenhuma.

Somente leitura: nada é executado, os comandos sugeridos são impressos para
você conferir e rodar.

Exemplos:

  heimdall pipeline diagnose --log build.log
  terraform plan 2>&1 | heimdall pipeline diagnose --log -`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiagnose(cmd, &opts)
		},
	}

	cmd.Flags().StringVar(&opts.logPath, "log", "", `arquivo de log ("-" lê do stdin)`)
	cmd.Flags().StringVarP(&opts.output, "output", "o", "text", "formato da saída: text ou json")
	cmd.MarkFlagRequired("log")

	return cmd
}

func runDiagnose(cmd *cobra.Command, opts *diagnoseOptions) error {
	if opts.output != "text" && opts.output != "json" {
		return fmt.Errorf("formato de saída inválido: %q (use text ou json)", opts.output)
	}

	in, closeIn, source, err := openLogInput(cmd, opts.logPath)
	if err != nil {
		return err
	}
	defer closeIn()

	diagnosis, err := pipelinedoctor.Diagnose(in, source)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if opts.output == "json" {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(diagnosis)
	}

	return pipelinedoctor.WriteReport(out, diagnosis)
}

func openLogInput(cmd *cobra.Command, path string) (io.Reader, func(), string, error) {
	if path == "-" {
		return cmd.InOrStdin(), func() {}, "stdin", nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, nil, "", fmt.Errorf("não foi possível ler o log: %w", err)
	}
	return f, func() { f.Close() }, path, nil
}
