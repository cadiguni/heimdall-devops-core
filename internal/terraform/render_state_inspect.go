package terraform

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// WriteStateReport escreve o resumo de um arquivo de state.
//
// Só endereço, tipo e provider — nunca valor de atributo. Um state guarda os
// atributos em texto puro, e é comum ter senha e connection string ali.
func WriteStateReport(w io.Writer, s *StateInspection) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "State\t%s\n", s.Path)
	if s.TerraformVersion != "" {
		fmt.Fprintf(tw, "Terraform\tv%s\n", s.TerraformVersion)
	}
	fmt.Fprintf(tw, "Serial\t%d\n", s.Serial)
	if s.Lineage != "" {
		fmt.Fprintf(tw, "Lineage\t%s\n", s.Lineage)
	}
	fmt.Fprintf(tw, "Recursos\t%d gerenciados, %d data sources\n", s.Managed, s.Data)
	if len(s.Outputs) > 0 {
		fmt.Fprintf(tw, "Outputs\t%s\n", strings.Join(s.Outputs, ", "))
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if s.Empty() {
		_, err := fmt.Fprintln(w, "\nEste state não rastreia recurso nenhum.\n"+
			"Ou nunca foi usado, ou a infraestrutura correspondente está sendo\n"+
			"rastreada em outro lugar.")
		return err
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	rt := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(rt, "ENDEREÇO\tPROVIDER")
	for _, r := range s.Resources {
		address := r.Address
		if r.Tainted {
			address += "  (tainted)"
		}
		fmt.Fprintf(rt, "%s\t%s\n", address, r.Provider)
	}
	return rt.Flush()
}
