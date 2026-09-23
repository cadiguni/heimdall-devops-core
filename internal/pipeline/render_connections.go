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
