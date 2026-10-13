package terraform

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// WriteRemapPlan escreve o plano de troca de assinatura.
func WriteRemapPlan(w io.Writer, p *RemapPlan, apply bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Alvo\n  operação\tremapear assinatura no state\n")
	fmt.Fprintf(tw, "  container\t%s\n", p.Container)
	fmt.Fprintf(tw, "  state\t%s\n", p.Key)
	if p.Environment != "" {
		fmt.Fprintf(tw, "  ambiente\t%s\n", p.Environment)
	}
	if p.ResourceGroup != "" {
		fmt.Fprintf(tw, "  resource group\t%s\n", p.ResourceGroup)
	}
	fmt.Fprintf(tw, "  de\t%s\n", p.From)
	fmt.Fprintf(tw, "  para\t%s\n", p.To)
	if err := tw.Flush(); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, "\nVerificações"); err != nil {
		return err
	}
	for _, c := range p.Checks {
		if _, err := fmt.Fprintf(w, "  [%s] %s: %s\n", checkMark(c.Status), c.Name, c.Detail); err != nil {
			return err
		}
	}

	if err := writeRemapTargets(w, p); err != nil {
		return err
	}
	if err := writeRemapOpaque(w, p); err != nil {
		return err
	}
	if err := writeRemapUntouched(w, p); err != nil {
		return err
	}

	if p.Blocked() {
		_, err := fmt.Fprintln(w, "\nRemap bloqueado. Nada foi executado.")
		return err
	}

	return writeRemapPlanTail(w, p, apply)
}

func writeRemapTargets(w io.Writer, p *RemapPlan) error {
	alvos := p.Targets()
	if len(alvos) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "\nSerão remapeados (%d)\n", len(alvos)); err != nil {
		return err
	}
	for _, e := range alvos {
		if _, err := fmt.Fprintf(w, "  %s\n    de   %s\n    para %s\n", e.Address, e.CurrentID, e.NewID); err != nil {
			return err
		}
	}
	return nil
}

// writeRemapOpaque é a seção que justifica a terceira categoria existir: a
// assinatura está no id, este comando não sabe reescrever, e ignorar deixaria
// a migração pela metade sem ninguém notar.
func writeRemapOpaque(w io.Writer, p *RemapPlan) error {
	opacos := p.Opaque()
	if len(opacos) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w,
		"\nMencionam a assinatura em formato não reescrevível (%d)\n", len(opacos)); err != nil {
		return err
	}
	for _, e := range opacos {
		if _, err := fmt.Fprintf(w, "  %s\n    %s\n", e.Address, e.Reason); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintln(w, "\nEsses precisam de tratamento à mão antes. O id guarda a assinatura em\n"+
		"base64, querystring ou formato próprio do provedor, e reescrever às cegas\n"+
		"produziria um id inválido.")
	return err
}

func writeRemapUntouched(w io.Writer, p *RemapPlan) error {
	intocados := p.Untouched()
	if len(intocados) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "\nNão serão tocados (%d)\n", len(intocados)); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, e := range intocados {
		fmt.Fprintf(tw, "  %s\t%s\n", e.Address, e.Reason)
	}
	return tw.Flush()
}

func writeRemapPlanTail(w io.Writer, p *RemapPlan, apply bool) error {
	if apply {
		_, err := fmt.Fprintf(w, "\nExecutando (--apply): %d recurso(s), um por vez.\n", len(p.Targets()))
		return err
	}

	if _, err := fmt.Fprintln(w, "\nDry-run: nada foi executado."); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, "\nO move no portal precisa ter acontecido ANTES. Se os recursos ainda\n"+
		"estiverem na assinatura antiga, o import não acha nada no id novo e o\n"+
		"state fica com buraco."); err != nil {
		return err
	}

	_, err := fmt.Fprintln(w, "\nPara executar, repita com --apply. Cada recurso é um 'state rm' seguido de\n"+
		"um 'import'; na primeira falha a execução para e diz onde parou.")
	return err
}
