package terraform

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// WriteCoverage escreve o cruzamento entre a assinatura e o container.
func WriteCoverage(w io.Writer, r *CoverageReport, mostrarTags bool) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Alvo\n  assinatura\t%s\n", r.Subscription)
	fmt.Fprintf(tw, "  container\t%s\n", r.Container)
	fmt.Fprintf(tw, "  varridos\t%d resource groups, %d states\n", r.ScannedGroups, r.ScannedStates)
	if err := tw.Flush(); err != nil {
		return err
	}

	if err := writeTracked(w, r); err != nil {
		return err
	}
	if err := writeUntracked(w, r, mostrarTags); err != nil {
		return err
	}
	if err := writeMissing(w, r); err != nil {
		return err
	}
	if err := writeCoverageNotes(w, r); err != nil {
		return err
	}

	return writeCoverageTail(w, r)
}

func writeTracked(w io.Writer, r *CoverageReport) error {
	tracked := r.ByStatus(CoverageTracked)
	if len(tracked) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "\nCom state (%d)\n", len(tracked)); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, e := range tracked {
		fmt.Fprintf(tw, "  %s\t%s\n", e.ResourceGroup, strings.Join(e.States, ", "))
	}
	return tw.Flush()
}

// writeUntracked é a seção que responde à pergunta de origem: o que existe na
// assinatura sem nenhum state por trás.
//
// As tags vão junto porque, sem state, elas são o que resta para dizer de quem
// é o recurso.
func writeUntracked(w io.Writer, r *CoverageReport, mostrarTags bool) error {
	untracked := r.ByStatus(CoverageUntracked)
	if len(untracked) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "\nSem state neste container (%d)\n", len(untracked)); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, e := range untracked {
		if mostrarTags {
			fmt.Fprintf(tw, "  %s\t%s\t%s\n", e.ResourceGroup, e.Location, formatTags(e.Tags))
			continue
		}
		fmt.Fprintf(tw, "  %s\t%s\n", e.ResourceGroup, e.Location)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	_, err := fmt.Fprintln(w, "\nIsso quer dizer que nenhum state DESTE container rastreia o resource group.\n"+
		"Pode ter sido criado à mão, ou ter state em outro container ou outra conta.\n"+
		"Antes de concluir, confira se o time usa outro backend.")
	return err
}

func writeMissing(w io.Writer, r *CoverageReport) error {
	missing := r.ByStatus(CoverageMissing)
	if len(missing) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w,
		"\nState rastreia resource group que não está na assinatura (%d)\n", len(missing)); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, e := range missing {
		fmt.Fprintf(tw, "  %s\t%s\n", e.ResourceGroup, strings.Join(e.States, ", "))
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	_, err := fmt.Fprintln(w, "\nEsses foram apagados ou movidos para outra assinatura. Movido é o caso do\n"+
		"'states remap'; apagado é state a aposentar.")
	return err
}

func writeCoverageNotes(w io.Writer, r *CoverageReport) error {
	if len(r.Notes) == 0 {
		return nil
	}
	if _, err := fmt.Fprintln(w, "\nLimites desta varredura"); err != nil {
		return err
	}
	for _, n := range r.Notes {
		if _, err := fmt.Fprintf(w, "  %s\n", n); err != nil {
			return err
		}
	}
	return nil
}

func writeCoverageTail(w io.Writer, r *CoverageReport) error {
	if r.ScannedGroups == 0 {
		_, err := fmt.Fprintln(w, "\nNenhum resource group na assinatura. Se isso surpreende, é mais provável\n"+
			"que seja a assinatura errada do que uma assinatura vazia.")
		return err
	}
	if r.ScannedStates == 0 {
		_, err := fmt.Fprintln(w, "\nNenhum state no container, então TODO resource group aparece como sem state.\n"+
			"Confira a conta e o container antes de ler isso como resultado.")
		return err
	}
	return nil
}

func formatTags(tags map[string]string) string {
	if len(tags) == 0 {
		return "sem tags"
	}

	chaves := make([]string, 0, len(tags))
	for k := range tags {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)

	partes := make([]string, 0, len(chaves))
	for _, k := range chaves {
		partes = append(partes, k+"="+tags[k])
	}
	return strings.Join(partes, " ")
}
