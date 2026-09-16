package terraform

import (
	"fmt"
	"io"
	"text/tabwriter"
)

func stateOpLabel(op StateOp) string {
	switch op {
	case StateOpRemove:
		return "remover do state"
	case StateOpMove:
		return "mover no state"
	default:
		return string(op)
	}
}

// WriteStateOpPreflight escreve o resultado das verificações de um rm ou mv.
func WriteStateOpPreflight(w io.Writer, p *StateOpPreflight, apply bool) error {
	if err := writeStateOpTarget(w, p); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w, "Verificações"); err != nil {
		return err
	}
	for _, c := range p.Checks {
		if _, err := fmt.Fprintf(w, "  [%s] %s: %s\n", checkMark(c.Status), c.Name, c.Detail); err != nil {
			return err
		}
	}

	if err := writeAffected(w, p); err != nil {
		return err
	}

	if p.Blocked() {
		_, err := fmt.Fprintf(w, "\n%s bloqueado. Nada foi executado.\n", stateOpLabel(p.Operation))
		return err
	}

	return writeStateOpPlan(w, p, apply)
}

func writeStateOpTarget(w io.Writer, p *StateOpPreflight) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Alvo\n  operação\t%s\n", stateOpLabel(p.Operation))
	fmt.Fprintf(tw, "  container\t%s\n", p.Container)
	fmt.Fprintf(tw, "  state\t%s\n", p.Key)
	if p.Environment != "" {
		fmt.Fprintf(tw, "  ambiente\t%s\n", p.Environment)
	}
	fmt.Fprintf(tw, "  endereço\t%s\n", p.Address)
	if p.Destination != "" {
		fmt.Fprintf(tw, "  destino\t%s\n", p.Destination)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	_, err := fmt.Fprintln(w)
	return err
}

// writeAffected mostra o que exatamente sai do state. É a diferença entre
// remover um recurso e remover dez sem perceber, quando o endereço não tem
// índice e o recurso usa count ou for_each.
func writeAffected(w io.Writer, p *StateOpPreflight) error {
	if len(p.Affected) == 0 {
		return nil
	}

	verbo := "Sai do state"
	if p.Operation == StateOpMove {
		verbo = "Muda de endereço"
	}

	if _, err := fmt.Fprintf(w, "\n%s (%d)\n", verbo, len(p.Affected)); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range p.Affected {
		address := r.Address
		if r.Tainted {
			address += "  (tainted)"
		}
		fmt.Fprintf(tw, "  %s\t%s\n", address, r.Provider)
	}
	return tw.Flush()
}

func writeStateOpPlan(w io.Writer, p *StateOpPreflight, apply bool) error {
	if apply {
		_, err := fmt.Fprintf(w, "\nExecutando (--apply).\n")
		return err
	}

	if _, err := fmt.Fprintln(w, "\nDry-run: nada foi executado."); err != nil {
		return err
	}

	if p.Operation == StateOpRemove {
		if _, err := fmt.Fprintln(w,
			"\nA infraestrutura no Azure continua existindo — só deixa de ser rastreada.\n"+
				"Depois disso, um plan vai querer criar o recurso de novo, a não ser que\n"+
				"ele saia da configuração também."); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintln(w, "\nO equivalente à mão, no diretório do módulo:"); err != nil {
		return err
	}
	for _, c := range p.Commands {
		if _, err := fmt.Fprintf(w, "  %s\n", c); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintln(w, "\nPara o heimdall executar, repita com --apply.\n"+
		"O 'init' continua sendo seu: reconfigurar backend de diretório alheio pode migrar state.")
	return err
}
