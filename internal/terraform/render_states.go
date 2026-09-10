package terraform

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
)

// lockLabel é o rótulo em português do estado do lock.
func lockLabel(l LockState) string {
	switch l {
	case LockStateFree:
		return "livre"
	case LockStateLocked:
		return "TRAVADO"
	case LockStateStaleMetadata:
		return "metadata órfã"
	default:
		return string(l)
	}
}

// WriteStatesReport escreve o inventário de states.
//
// O cabeçalho sempre diz contra qual container a leitura foi feita: os states
// de dev, hml e prod só se distinguem pelo caminho, e confundi-los é o erro
// caro deste fluxo.
func WriteStatesReport(w io.Writer, inv *StateInventory, now time.Time) error {
	if _, err := fmt.Fprintf(w, "Container: %s\n", inv.Container); err != nil {
		return err
	}
	if inv.Prefix != "" {
		if _, err := fmt.Fprintf(w, "Prefixo:   %s\n", inv.Prefix); err != nil {
			return err
		}
	}

	if len(inv.States) == 0 {
		_, err := fmt.Fprintln(w, "\nNenhum state encontrado.")
		return err
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "AMBIENTE\tSTATE\tMODIFICADO (UTC)\tTAMANHO\tLOCK")
	for _, s := range inv.States {
		env := s.Environment
		if env == "" {
			env = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			env,
			s.Path,
			s.LastModified.UTC().Format("2006-01-02 15:04"),
			humanSize(s.SizeBytes),
			lockLabel(s.Lock),
		)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	return writeLockDetails(w, inv, now)
}

func writeLockDetails(w io.Writer, inv *StateInventory, now time.Time) error {
	locked := inv.Locked()
	if len(locked) == 0 {
		if _, err := fmt.Fprintln(w, "\nNenhum state travado."); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, "\nStates travados (%d)\n", len(locked)); err != nil {
			return err
		}
		for _, s := range locked {
			if _, err := fmt.Fprintf(w, "  %s%s\n", s.Path, lockDetail(s, now)); err != nil {
				return err
			}
		}
	}

	return writeStaleWarning(w, inv)
}

// lockDetail descreve quem travou, sem inventar dado que não veio: parte da
// metadata pode estar ausente conforme a versão do Terraform.
func lockDetail(s StateEntry, now time.Time) string {
	if s.LockInfo == nil {
		return " — lease ativo, sem metadata de lock legível"
	}

	var parts []string
	if s.LockInfo.Operation != "" {
		parts = append(parts, s.LockInfo.Operation)
	}
	if s.LockInfo.Who != "" {
		parts = append(parts, "por "+s.LockInfo.Who)
	}
	if d := s.LockedFor(now); d > 0 {
		parts = append(parts, "há "+humanDuration(d))
	}
	if s.LockInfo.ID != "" {
		parts = append(parts, "id "+s.LockInfo.ID)
	}

	if len(parts) == 0 {
		return ""
	}
	return " — " + strings.Join(parts, ", ")
}

func writeStaleWarning(w io.Writer, inv *StateInventory) error {
	var stale []string
	for _, s := range inv.States {
		if s.Lock == LockStateStaleMetadata {
			stale = append(stale, s.Path)
		}
	}
	if len(stale) == 0 {
		return nil
	}

	_, err := fmt.Fprintf(w,
		"\nAVISO: %d state(s) com metadata de lock sem lease ativo:\n  %s\n"+
			"Não estão travados. Costuma ser resquício de execução interrompida.\n",
		len(stale), strings.Join(stale, "\n  "))
	return err
}

func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit && exp < 3; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGT"[exp])
}

// humanDuration formata a duração do jeito que interessa para "esse lock está
// preso?": horas e minutos, sem segundos.
func humanDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60

	if h == 0 {
		return fmt.Sprintf("%dmin", m)
	}
	if h < 24 {
		return fmt.Sprintf("%dh%02dmin", h, m)
	}
	return fmt.Sprintf("%dd%02dh", h/24, h%24)
}
