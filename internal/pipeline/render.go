package pipeline

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// evidenceMaxLen corta a evidência na saída de texto. A mensagem do CDN passa
// de 700 caracteres em uma linha só e espremeria o resto do relatório.
const evidenceMaxLen = 140

// group junta as ocorrências de uma mesma assinatura.
type group struct {
	signature Finding
	findings  []Finding
}

// groupFindings agrupa por assinatura preservando a ordem de primeira
// aparição. Um log com seis variáveis não declaradas é um problema, não seis.
func groupFindings(findings []Finding) []group {
	var groups []group
	index := map[string]int{}

	for _, f := range findings {
		if pos, seen := index[f.Signature]; seen {
			groups[pos].findings = append(groups[pos].findings, f)
			continue
		}
		index[f.Signature] = len(groups)
		groups = append(groups, group{signature: f, findings: []Finding{f}})
	}

	// Erros antes de avisos. Um aviso que apareceu na linha 20 não pode
	// encabeçar o relatório de uma pipeline que quebrou na linha 200.
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i].signature.Severity == SeverityError &&
			groups[j].signature.Severity != SeverityError
	})

	return groups
}

func severityLabel(s Severity) string {
	switch s {
	case SeverityError:
		return "erro"
	case SeverityWarning:
		return "aviso"
	default:
		return string(s)
	}
}

// WriteReport escreve o diagnóstico legível.
func WriteReport(w io.Writer, d *Diagnosis) error {
	if d.Source != "" {
		if _, err := fmt.Fprintf(w, "Log: %s (%d linhas)\n", d.Source, d.LinesScanned); err != nil {
			return err
		}
	}

	if !d.HasFindings() {
		_, err := fmt.Fprintln(w, "\nNenhuma falha conhecida reconhecida neste log.\n"+
			"Isso não quer dizer que está tudo bem: quer dizer que o padrão não está no catálogo.")
		return err
	}

	erros := len(d.Errors())
	if _, err := fmt.Fprintf(w, "\n%d achado(s): %d erro(s), %d aviso(s)\n",
		len(d.Findings), erros, len(d.Findings)-erros); err != nil {
		return err
	}

	for _, g := range groupFindings(d.Findings) {
		if err := writeGroup(w, g); err != nil {
			return err
		}
	}

	return nil
}

func writeGroup(w io.Writer, g group) error {
	sig := g.signature

	if _, err := fmt.Fprintf(w, "\n[%s] %s  (%s)\n", severityLabel(sig.Severity), sig.Title, sig.Signature); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Causa: %s\n", sig.Cause); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  O que fazer: %s\n", sig.Action); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "  Ocorrências (%d):\n", len(g.findings)); err != nil {
		return err
	}
	for _, f := range g.findings {
		if _, err := fmt.Fprintf(w, "    linha %d: %s\n", f.Line, truncate(f.Evidence, evidenceMaxLen)); err != nil {
			return err
		}
		if detail := formatDetails(f.Details); detail != "" {
			if _, err := fmt.Fprintf(w, "      %s\n", detail); err != nil {
				return err
			}
		}
	}

	return writeCommands(w, g)
}

func writeCommands(w io.Writer, g group) error {
	var commands []string
	seen := map[string]bool{}
	for _, f := range g.findings {
		for _, c := range f.Commands {
			if !seen[c] {
				seen[c] = true
				commands = append(commands, c)
			}
		}
	}
	if len(commands) == 0 {
		return nil
	}

	if _, err := fmt.Fprintln(w, "  Comandos sugeridos (confira antes de rodar):"); err != nil {
		return err
	}
	for _, c := range commands {
		if _, err := fmt.Fprintf(w, "    %s\n", c); err != nil {
			return err
		}
	}
	return nil
}

// formatDetails serializa os detalhes em ordem estável, para a saída não mudar
// entre execuções por causa da iteração de mapa.
func formatDetails(details map[string]string) string {
	if len(details) == 0 {
		return ""
	}

	keys := make([]string, 0, len(details))
	for k := range details {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, details[k]))
	}
	return strings.Join(parts, "  ")
}

// truncate corta por runes, não por bytes: o log tem acentos e caracteres de
// moldura, e cortar no meio de um deles produziria lixo na saída.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
