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

	if err := writeDrift(w, r); err != nil {
		return err
	}

	if r.IsIncomplete() {
		_, err := fmt.Fprintln(w, "\nAVISO: plano incompleto (uso de -target ou mudanças adiadas).\n"+
			"Recursos fora do plano podem conter destruição não listada aqui.")
		return err
	}

	return nil
}

// driftLabel descreve o que o refresh encontrou, em vez do que o plano vai
// fazer: aqui a ação já aconteceu, por fora do Terraform.
func driftLabel(k ChangeKind) string {
	switch k {
	case KindDelete:
		return "sumiu"
	case KindUpdate:
		return "alterado"
	case KindCreate:
		return "apareceu"
	case KindReplace:
		return "recriado"
	default:
		return kindLabel(k)
	}
}

// writeDrift lista o que mudou por fora do Terraform. Não é o que o plano vai
// fazer — é o que já aconteceu sem passar por ele.
func writeDrift(w io.Writer, r *PlanReview) error {
	if !r.HasDrift() {
		return nil
	}

	if _, err := fmt.Fprintf(w, "\nMudou fora do Terraform (%d)\n", len(r.Drift)); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, rc := range r.Drift {
		fmt.Fprintf(tw, "  %s\t%s\n", driftLabel(rc.Kind), rc.Address)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	_, err := fmt.Fprintln(w, "\nO apply vai reverter isso. Se a mudança manual era intencional,\n"+
		"ela precisa entrar na configuração antes.")
	return err
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
