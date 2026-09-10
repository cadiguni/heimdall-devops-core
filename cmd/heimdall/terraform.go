package main

import (
	"github.com/spf13/cobra"
)

// newTerraformCmd é o grupo do módulo Terraform Doctor.
// Por enquanto só expõe --help; os subcomandos (a começar por plan-review)
// entram conforme internal/terraform for implementado.
func newTerraformCmd(_ *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "terraform",
		Aliases: []string{"tf"},
		Short:   "Diagnóstico e correção de Terraform (drift, state, import)",
		Long: `Módulo Terraform Doctor.

Nenhum subcomando implementado ainda. O primeiro será 'plan-review',
que analisa a saída de 'terraform show -json' de um plano.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	return cmd
}
