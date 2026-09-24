package pipeline

import (
	"fmt"
	"io"
	"text/tabwriter"
)

func lifetimeLabel(l CredentialLifetime) string {
	switch l {
	case LifetimeExpires:
		return "vence"
	case LifetimePermanent:
		return "não vence"
	case LifetimeUnknown:
		return "?"
	default:
		return string(l)
	}
}

// WriteConnections escreve o inventário de service connections.
func WriteConnections(w io.Writer, inv *ConnectionInventory) error {
	if _, err := fmt.Fprintf(w, "Projeto: %s\n", inv.Project); err != nil {
		return err
	}

	if len(inv.Connections) == 0 {
		_, err := fmt.Fprintln(w, "\nNenhuma service connection encontrada.")
		return err
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NOME\tTIPO\tAUTENTICAÇÃO\tCREDENCIAL\tPRONTA")
	for _, c := range inv.Connections {
		pronta := "sim"
		if !c.Ready {
			pronta = "NÃO"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			c.Name, c.Type, c.Scheme, lifetimeLabel(c.Lifetime), pronta)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	if err := writeNotReady(w, inv); err != nil {
		return err
	}
	if err := writeDuplicates(w, inv); err != nil {
		return err
	}
	return writeExpiring(w, inv)
}

func writeNotReady(w io.Writer, inv *ConnectionInventory) error {
	quebradas := inv.NotReady()
	if len(quebradas) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "\nConexões que o Azure DevOps não considera prontas (%d)\n", len(quebradas)); err != nil {
		return err
	}
	for _, c := range quebradas {
		if _, err := fmt.Fprintf(w, "  %s\n", c.Name); err != nil {
			return err
		}
	}
	return nil
}

// writeDuplicates fala em suspeita, não em duplicata: duas conexões para a
// mesma assinatura podem existir de propósito, e só quem conhece o projeto
// decide qual apagar.
func writeDuplicates(w io.Writer, inv *ConnectionInventory) error {
	if len(inv.Duplicates) == 0 {
		return nil
	}

	if _, err := fmt.Fprintf(w, "\nSuspeitas de duplicata: mesmo tipo e mesma assinatura no nome (%d)\n", len(inv.Duplicates)); err != nil {
		return err
	}
	for _, g := range inv.Duplicates {
		if _, err := fmt.Fprintf(w, "  %s, assinatura %s\n", g.Type, g.Subscription); err != nil {
			return err
		}
		for _, name := range g.Names {
			if _, err := fmt.Fprintf(w, "    %s\n", name); err != nil {
				return err
			}
		}
	}

	_, err := fmt.Fprintln(w, "\nPode ser intencional (escopos ou permissões diferentes). Quando não for, cada\n"+
		"conexão a menos é um segredo a menos para renovar. Antes de apagar, confira\n"+
		"quais pipelines usam cada uma.")
	return err
}

// writeExpiring é o ponto do comando: separar o que depende de segredo com
// validade do que não depende de nada.
//
// A data de validade não sai daqui porque não existe na API do Azure DevOps.
// Dizer isso explicitamente vale mais do que omitir: quem lê precisa saber que
// a resposta está em outro lugar, não que o comando esqueceu de mostrar.
func writeExpiring(w io.Writer, inv *ConnectionInventory) error {
	vencem := inv.Expiring()
	if len(vencem) == 0 {
		_, err := fmt.Fprintln(w, "\nNenhuma conexão depende de segredo com validade.")
		return err
	}

	if _, err := fmt.Fprintf(w, "\nDependem de segredo com validade (%d)\n", len(vencem)); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range vencem {
		fmt.Fprintf(tw, "  %s\t%s\n", c.Name, c.Scheme)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	_, err := fmt.Fprintln(w, "\nA data de validade não está na API do Azure DevOps: o segredo vive no\n"+
		"Entra ID (app registration) ou no provedor externo. Conexão do Azure por\n"+
		"federação de identidade de carga de trabalho não tem segredo e não vence —\n"+
		"migrar resolve o problema em vez de adiá-lo.")
	return err
}
