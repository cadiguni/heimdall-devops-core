package pipeline

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// groupTypeLabel traduz o tipo do grupo. "Vsts" é o nome interno do grupo comum
// e não diz nada a quem lê.
func groupTypeLabel(t string) string {
	switch t {
	case "Vsts":
		return "comum"
	case "AzureKeyVault":
		return "Key Vault"
	default:
		return t
	}
}

// WriteVariableGroups escreve o inventário de Variable Groups do projeto.
func WriteVariableGroups(w io.Writer, inv *VariableGroupInventory) error {
	if _, err := fmt.Fprintf(w, "Projeto: %s\n", inv.Project); err != nil {
		return err
	}

	if len(inv.Groups) == 0 {
		_, err := fmt.Fprintln(w, "\nNenhum Variable Group encontrado.")
		return err
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNOME\tTIPO\tVARIÁVEIS\tSECRETAS\tMODIFICADO (UTC)")
	for _, g := range inv.Groups {
		modificado := "-"
		if !g.ModifiedOn.IsZero() {
			modificado = g.ModifiedOn.UTC().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%d\t%s\n",
			g.ID, g.Name, groupTypeLabel(g.Type), g.Total, g.Secrets, modificado)
	}
	return tw.Flush()
}

// WriteVariableGroupDetail escreve as variáveis de um grupo.
//
// Sem --show-values sai só o nome. Um Variable Group é um repositório de
// segredos, e mesmo a variável não marcada como secreta costuma guardar coisa
// que não devia ir para log de pipeline.
func WriteVariableGroupDetail(w io.Writer, d *VariableGroupDetail) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "Variable Group\t%s (id %d)\n", d.Name, d.ID)
	fmt.Fprintf(tw, "Tipo\t%s\n", groupTypeLabel(d.Type))
	if d.Description != "" {
		fmt.Fprintf(tw, "Descrição\t%s\n", d.Description)
	}
	if d.Shared {
		fmt.Fprintf(tw, "Compartilhado\tsim, com outros projetos\n")
	}
	fmt.Fprintf(tw, "Variáveis\t%d, sendo %d secretas\n", d.Total, d.Secrets)
	if d.ModifiedBy != "" {
		fmt.Fprintf(tw, "Modificado por\t%s\n", d.ModifiedBy)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if len(d.Variables) == 0 {
		_, err := fmt.Fprintln(w, "\nO grupo não tem variáveis.")
		return err
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	vt := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if d.ValuesRevealed {
		fmt.Fprintln(vt, "VARIÁVEL\tSECRETA\tVALOR")
	} else {
		fmt.Fprintln(vt, "VARIÁVEL\tSECRETA")
	}

	for _, v := range d.Variables {
		secreta := "não"
		if v.Secret {
			secreta = "SIM"
		}
		if v.ReadOnly {
			secreta += " (somente leitura)"
		}

		if !d.ValuesRevealed {
			fmt.Fprintf(vt, "%s\t%s\n", v.Name, secreta)
			continue
		}
		fmt.Fprintf(vt, "%s\t%s\t%s\n", v.Name, secreta, valueOrPlaceholder(v))
	}
	if err := vt.Flush(); err != nil {
		return err
	}

	return writeValuesNote(w, d)
}

// valueOrPlaceholder nunca inventa valor para variável secreta: a API devolve
// null nessas, então não há o que mostrar mesmo com a revelação ligada.
func valueOrPlaceholder(v Variable) string {
	if v.Secret {
		return "(secreta, não devolvida pela API)"
	}
	return v.Value
}

func writeValuesNote(w io.Writer, d *VariableGroupDetail) error {
	if d.ValuesRevealed {
		_, err := fmt.Fprintln(w, "\nValores revelados por --show-values. Isto não deve ir para log de pipeline:\n"+
			"variável não marcada como secreta frequentemente guarda coisa que deveria ser.")
		return err
	}

	if d.Secrets == d.Total {
		return nil
	}

	_, err := fmt.Fprintln(w, "\nValores omitidos. Use --show-values para ver os das variáveis não secretas.")
	return err
}
