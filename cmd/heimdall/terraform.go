package main

import (
	"github.com/spf13/cobra"
)

// newTerraformCmd é o grupo do módulo Terraform Doctor.
func newTerraformCmd(gf *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "terraform",
		Aliases: []string{"tf"},
		Short:   "Diagnóstico e correção de Terraform (drift, state, import)",
		Long: `Módulo Terraform Doctor.

Subcomando disponível: plan-review, que analisa a saída de
'terraform show -json' e destaca as operações destrutivas do plano.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newPlanReviewCmd(gf))

	return cmd
}
