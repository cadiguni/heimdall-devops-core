package pipeline

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// WriteReferences escreve o relatório de onde a assinatura aparece.
//
// Relatório, nunca escrita: o heimdall diz onde está, e quem altera é quem
// conhece o efeito de alterar.
func WriteReferences(w io.Writer, r *ReferenceReport) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Alvo\n  assinatura\t%s\n", r.Subscription)
	if r.Project != "" {
		fmt.Fprintf(tw, "  projeto\t%s\n", r.Project)
	}
	if r.RepoDir != "" {
		fmt.Fprintf(tw, "  repositório\t%s\n", r.RepoDir)
	}
	fmt.Fprintf(tw, "  varridos\t%d service connections, %d Variable Groups, %d arquivos\n",
		r.ScannedConnections, r.ScannedGroups, r.ScannedFiles)
	if err := tw.Flush(); err != nil {
		return err
	}

	if !r.Found() {
		if _, err := fmt.Fprintln(w, "\nNenhuma referência encontrada."); err != nil {
			return err
		}
	}

	for _, s := range []struct {
		source RefSource
		titulo string
	}{
		{SourceConnection, "Service connections"},
		{SourceVariableGroup, "Variable Groups (library)"},
		{SourceRepo, "Repositório"},
	} {
		if err := writeRefSection(w, s.titulo, r.FromSource(s.source)); err != nil {
			return err
		}
	}

	if err := writeSecretBlind(w, r); err != nil {
		return err
	}
	if err := writeRefNotes(w, r); err != nil {
		return err
	}

	return writeRefTail(w, r)
}

func writeRefSection(w io.Writer, titulo string, refs []Reference) error {
	if len(refs) == 0 {
		return nil
	}
	SortReferences(refs)

	if _, err := fmt.Fprintf(w, "\n%s (%d)\n", titulo, len(refs)); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, ref := range refs {
		onde := ref.Where
		if ref.Line > 0 {
			onde = fmt.Sprintf("%s:%d", ref.Where, ref.Line)
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", onde, ref.What, ref.Detail)
	}
	return tw.Flush()
}

// writeSecretBlind é a seção que impede o relatório de mentir por omissão: a
// API devolve null no valor de variável secreta, então não há como saber se ela
// cita a assinatura. Contar como limpa seria afirmar o que não se sabe.
func writeSecretBlind(w io.Writer, r *ReferenceReport) error {
	if len(r.SecretBlind) == 0 {
		return nil
	}
	SortReferences(r.SecretBlind)

	if _, err := fmt.Fprintf(w,
		"\nNão verificáveis: variáveis secretas (%d)\n", len(r.SecretBlind)); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, ref := range r.SecretBlind {
		fmt.Fprintf(tw, "  %s\t%s\n", ref.Where, ref.What)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	_, err := fmt.Fprintln(w, "\nA API do Azure DevOps devolve null no valor dessas, então esta varredura não\n"+
		"alcança o conteúdo delas. Se alguma guarda a assinatura, só quem a definiu sabe.")
	return err
}

func writeRefNotes(w io.Writer, r *ReferenceReport) error {
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

func writeRefTail(w io.Writer, r *ReferenceReport) error {
	if !r.Found() {
		if r.ScannedGroups == 0 && r.ScannedConnections == 0 && r.ScannedFiles == 0 {
			_, err := fmt.Fprintln(w, "\nNada foi varrido, então isto não é uma resposta. Confira --org/--project\n"+
				"e o --chdir.")
			return err
		}
		return nil
	}

	// Nenhum valor aparece acima, e o relatório precisa dizer por quê: senão
	// parece que a informação faltou.
	if _, err := fmt.Fprintln(w, "\nO valor de cada achado não é impresso de propósito — ele pode ser uma\n"+
		"connection string com a assinatura no meio, e esta saída acaba em log de\n"+
		"pipeline. O que vai acima é onde abrir."); err != nil {
		return err
	}

	_, err := fmt.Fprintln(w, "\nNada foi alterado. O heimdall não escreve em Variable Group nem em service\n"+
		"connection: a alteração é sua.")
	return err
}
