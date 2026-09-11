package main

import (
	"github.com/spf13/cobra"
)

// version é sobrescrita no build via -ldflags "-X main.version=...".
var version = "dev"

// flags globais compartilhadas por todos os subcomandos.
type globalFlags struct {
	// verbose habilita saída de diagnóstico detalhada.
	verbose bool
}

func newRootCmd() *cobra.Command {
	var gf globalFlags

	cmd := &cobra.Command{
		Use:   "heimdall",
		Short: "Automação e diagnóstico de DevOps (Terraform, Azure DevOps, Azure)",
		Long: `heimdall automatiza diagnóstico e correção de problemas de DevOps:
drift e state de Terraform, pipelines e templates de Azure DevOps,
Variable Groups e recursos Azure.

Toda operação potencialmente destrutiva roda em dry-run por padrão.`,
		Version: version,
		// Sem subcomando, mostra o help em vez de sair silenciosamente.
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().BoolVarP(&gf.verbose, "verbose", "v", false, "saída de diagnóstico detalhada")

	cmd.AddCommand(newTerraformCmd(&gf))
	cmd.AddCommand(newPipelineCmd(&gf))

	return cmd
}
