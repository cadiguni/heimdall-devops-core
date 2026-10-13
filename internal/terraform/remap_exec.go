package terraform

import (
	"context"
	"fmt"
	"io"
)

// RemapResult é o que aconteceu numa execução de remap.
type RemapResult struct {
	// Done são os recursos já remapeados, em ordem.
	Done []string

	// FailedAt é o endereço em que a execução parou, vazio se terminou.
	FailedAt string

	// Phase diz em qual metade da operação a falha aconteceu: o recurso ficou
	// fora do state ("rm") ou nunca saiu ("import"). Muda o que é preciso
	// fazer para retomar.
	Phase string
}

// ExecuteRemap troca a assinatura de cada recurso, um por vez.
//
// Não é atômico e não tenta ser: 'terraform state rm' e 'import' são duas
// chamadas, e não existe transação entre elas. Em vez de fingir atomicidade, a
// execução para na primeira falha e devolve exatamente onde parou — quem chama
// imprime o comando de retomada.
//
// A ordem é rm e depois import, porque o endereço precisa estar livre para o
// import entrar. A consequência é que uma falha no import deixa o recurso fora
// do state, existindo no Azure. É recuperável e precisa estar visível.
func ExecuteRemap(ctx context.Context, runner Runner, targets []RemapEntry, progress io.Writer) (*RemapResult, error) {
	result := &RemapResult{}

	for _, t := range targets {
		if progress != nil {
			fmt.Fprintf(progress, "  %s\n", t.Address)
		}

		if err := runner.StateRm(ctx, t.Address); err != nil {
			result.FailedAt = t.Address
			result.Phase = "rm"
			return result, fmt.Errorf("removendo %s do state: %w", t.Address, err)
		}

		if err := runner.Import(ctx, t.Address, t.NewID); err != nil {
			result.FailedAt = t.Address
			result.Phase = "import"
			return result, fmt.Errorf("importando %s com o id novo: %w", t.Address, err)
		}

		result.Done = append(result.Done, t.Address)
	}

	return result, nil
}

// WriteRemapResult descreve o que foi feito, e o que falta quando parou.
func WriteRemapResult(w io.Writer, r *RemapResult, targets []RemapEntry) error {
	if r.FailedAt == "" {
		_, err := fmt.Fprintf(w, "\n%d recurso(s) remapeados.\n", len(r.Done))
		return err
	}

	if _, err := fmt.Fprintf(w, "\nParou em %s, na fase de %s. %d já remapeado(s).\n",
		r.FailedAt, r.Phase, len(r.Done)); err != nil {
		return err
	}

	if r.Phase == "import" {
		if _, err := fmt.Fprintf(w,
			"\nEste recurso saiu do state e continua existindo no Azure. Para retomar:\n"+
				"  terraform import '%s' '%s'\n", r.FailedAt, newIDOf(targets, r.FailedAt)); err != nil {
			return err
		}
	}

	pendentes := remaining(targets, r.Done, r.FailedAt)
	if len(pendentes) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "\nFaltam %d recurso(s) depois dele:\n", len(pendentes)); err != nil {
		return err
	}
	for _, e := range pendentes {
		if _, err := fmt.Fprintf(w, "  %s\n", e.Address); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintln(w, "\nResolvido o que travou, rode o mesmo comando outra vez: os já remapeados\n"+
		"não aparecem mais no plano, porque o id deles deixou de ter a assinatura antiga.")
	return err
}

func newIDOf(targets []RemapEntry, address string) string {
	for _, t := range targets {
		if t.Address == address {
			return t.NewID
		}
	}
	return ""
}

// remaining são os alvos depois do que falhou, na ordem original.
func remaining(targets []RemapEntry, done []string, failedAt string) []RemapEntry {
	concluido := map[string]bool{}
	for _, d := range done {
		concluido[d] = true
	}

	var pendentes []RemapEntry
	for _, t := range targets {
		if concluido[t.Address] || t.Address == failedAt {
			continue
		}
		pendentes = append(pendentes, t)
	}
	return pendentes
}
