package terraform

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// kindLabel é o rótulo em português usado na saída de texto.
func kindLabel(k ChangeKind) string {
	switch k {
	case KindCreate:
		return "criar"
	case KindUpdate:
		return "alterar"
	case KindDelete:
		return "destruir"
	case KindReplace:
		return "recriar"
	case KindForget:
		return "esquecer"
	case KindNoOp:
		return "sem mudança"
	case KindRead:
		return "ler"
	default:
		return string(k)
	}
}

// WriteTextReport escreve o relatório legível da revisão.
//
// Só imprime endereços, tipos e operações — nunca valores de atributos — para
// que a saída possa ir para o log de um pipeline sem risco de vazar secrets.
func WriteTextReport(w io.Writer, r *PlanReview) error {
	if r.TerraformVersion != "" {
		if _, err := fmt.Fprintf(w, "Plano gerado por Terraform v%s\n\n", r.TerraformVersion); err != nil {
			return err
		}
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Resumo (recursos gerenciados)")
	fmt.Fprintf(tw, "  criar\t%d\n", r.Summary.Create)
	fmt.Fprintf(tw, "  alterar\t%d\n", r.Summary.Update)
	fmt.Fprintf(tw, "  destruir\t%d\n", r.Summary.Delete)
	fmt.Fprintf(tw, "  recriar\t%d\n", r.Summary.Replace)
	if r.Summary.Forget > 0 {
		fmt.Fprintf(tw, "  esquecer (sai do state)\t%d\n", r.Summary.Forget)
	}
	if r.Summary.DataReads > 0 {
		fmt.Fprintf(tw, "  data sources a ler\t%d\n", r.Summary.DataReads)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if r.Summary.Changed() == 0 {
		if _, err := fmt.Fprintln(w, "\nNenhuma mudança planejada."); err != nil {
			return err
		}
	}

	if err := writeDestructive(w, r); err != nil {
		return err
	}

	if r.Incomplete != nil && *r.Incomplete {
		_, err := fmt.Fprintln(w, "\nAVISO: plano incompleto (uso de -target ou mudanças adiadas).\n"+
			"Recursos fora do plano podem conter destruição não listada aqui.")
		return err
	}

	return nil
}

func writeDestructive(w io.Writer, r *PlanReview) error {
	if !r.HasDestructive() {
		if r.Summary.Changed() > 0 {
			if _, err := fmt.Fprintln(w, "\nNenhuma operação destrutiva."); err != nil {
				return err
			}
		}
		return nil
	}

	if _, err := fmt.Fprintf(w, "\nOperações destrutivas (%d)\n", len(r.Destructive)); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, rc := range r.Destructive {
		detail := rc.Reason
		if rc.CreateBeforeDestroy {
			if detail == "" {
				detail = "create-before-destroy"
			} else {
				detail += "; create-before-destroy"
			}
		}
		if detail != "" {
			detail = "(" + detail + ")"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", kindLabel(rc.Kind), rc.Address, detail)
	}
	return tw.Flush()
}
