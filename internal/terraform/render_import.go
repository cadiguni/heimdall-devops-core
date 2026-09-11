package terraform

import (
	"fmt"
	"io"
)

func checkMark(s CheckStatus) string {
	switch s {
	case CheckOK:
		return "ok  "
	case CheckWarn:
		return "!   "
	case CheckFail:
		return "FALHA"
	default:
		return string(s)
	}
}

// WriteImportPreflight escreve o resultado das verificações.
//
// O alvo vem primeiro e completo: o mesmo endereço de recurso existe em dev,
// hml e prod, e a única coisa que distingue um import certo de um desastre é
// contra qual state ele roda.
func WriteImportPreflight(w io.Writer, p *ImportPreflight, apply bool) error {
	if _, err := fmt.Fprintf(w,
		"Alvo\n  container  %s\n  state      %s\n", p.Container, p.Key); err != nil {
		return err
	}
	if p.Environment != "" {
		if _, err := fmt.Fprintf(w, "  ambiente   %s\n", p.Environment); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "  endereço   %s\n  recurso    %s\n\n", p.Address, p.ResourceID); err != nil {
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

	if p.Blocked() {
		_, err := fmt.Fprintln(w, "\nImport bloqueado. Nada foi executado.")
		return err
	}

	return writeImportPlan(w, p, apply)
}

func writeImportPlan(w io.Writer, p *ImportPreflight, apply bool) error {
	if apply {
		_, err := fmt.Fprintf(w, "\nExecutando o import (--apply).\n")
		return err
	}

	if _, err := fmt.Fprintln(w, "\nDry-run: nada foi executado."); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "\nO equivalente à mão, no diretório do módulo:"); err != nil {
		return err
	}
	for _, c := range p.Commands {
		if _, err := fmt.Fprintf(w, "  %s\n", c); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintln(w, "\nPara o heimdall executar o import, repita com --apply.\n"+
		"O 'init' continua sendo seu: reconfigurar backend de diretório alheio pode migrar state.")
	return err
}
