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

Subcomandos disponíveis:

  plan-review  analisa a saída de 'terraform show -json' e destaca as
               operações destrutivas do plano
  states       lista os states de um container do Azure e o lock de cada um
  import       verifica e executa um import contra o state certo`,
		// Sem NoArgs, um subcomando errado seria engolido como argumento e o
		// erro sairia sobre a flag seguinte, escondendo a causa.
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newPlanReviewCmd(gf))
	cmd.AddCommand(newStatesCmd(gf))
	cmd.AddCommand(newImportCmd(gf))

	return cmd
}
