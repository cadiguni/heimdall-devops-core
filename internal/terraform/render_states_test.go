package terraform

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func renderStates(t *testing.T, inv *StateInventory, now time.Time) string {
	t.Helper()

	var buf bytes.Buffer
	if err := WriteStatesReport(&buf, inv, now); err != nil {
		t.Fatalf("WriteStatesReport: %v", err)
	}
	return buf.String()
}

func TestWriteStatesReportListaEDetalhaLock(t *testing.T) {
	criado := time.Date(2026, 9, 10, 11, 0, 0, 0, time.UTC)
	agora := criado.Add(3*time.Hour + 12*time.Minute)

	inv := &StateInventory{
		Container: "https://stterraform.blob.core.windows.net/time1",
		States: []StateEntry{
			{
				Path: "dev/app.tfstate", Environment: "dev", Name: "app.tfstate",
				LastModified: criado, SizeBytes: 4096, Lock: LockStateFree,
			},
			{
				Path: "prod/app.tfstate", Environment: "prod", Name: "app.tfstate",
				LastModified: criado, SizeBytes: 120000, Lock: LockStateLocked,
				LockInfo: &LockInfo{
					ID: "4a1e6bd4", Operation: "OperationTypePlan",
					Who: "lucas@vm-build", Created: &criado,
				},
			},
		},
	}

	out := renderStates(t, inv, agora)

	for _, want := range []string{
		"https://stterraform.blob.core.windows.net/time1", // sempre diz contra o quê
		"dev/app.tfstate",
		"livre",
		"prod/app.tfstate",
		"TRAVADO",
		"States travados (1)",
		"OperationTypePlan",
		"lucas@vm-build",
		"há 3h12min",
		"id 4a1e6bd4",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q:\n%s", want, out)
		}
	}
}

func TestWriteStatesReportSemStates(t *testing.T) {
	out := renderStates(t, &StateInventory{Container: "https://x/y", States: nil}, time.Now())

	if !strings.Contains(out, "Nenhum state encontrado.") {
		t.Errorf("faltou a mensagem de container vazio:\n%s", out)
	}
}

func TestWriteStatesReportSemLocks(t *testing.T) {
	inv := &StateInventory{
		Container: "https://x/y",
		States:    []StateEntry{{Path: "dev/a.tfstate", Lock: LockStateFree}},
	}

	if out := renderStates(t, inv, time.Now()); !strings.Contains(out, "Nenhum state travado.") {
		t.Errorf("faltou a confirmação de ausência de locks:\n%s", out)
	}
}

func TestWriteStatesReportAvisaMetadataOrfa(t *testing.T) {
	inv := &StateInventory{
		Container: "https://x/y",
		States: []StateEntry{
			{Path: "hml/orfao.tfstate", Lock: LockStateStaleMetadata, LockInfo: &LockInfo{ID: "abc"}},
		},
	}

	out := renderStates(t, inv, time.Now())

	if !strings.Contains(out, "metadata de lock sem lease ativo") {
		t.Errorf("faltou o aviso de metadata órfã:\n%s", out)
	}
	if !strings.Contains(out, "Não estão travados") {
		t.Errorf("o aviso precisa deixar claro que não é lock:\n%s", out)
	}
}

func TestWriteStatesReportLockSemMetadata(t *testing.T) {
	inv := &StateInventory{
		Container: "https://x/y",
		States:    []StateEntry{{Path: "prod/a.tfstate", Lock: LockStateLocked}},
	}

	out := renderStates(t, inv, time.Now())

	if !strings.Contains(out, "sem metadata de lock legível") {
		t.Errorf("faltou explicar o lock sem metadata:\n%s", out)
	}
}

func TestWriteStatesReportMostraPrefixo(t *testing.T) {
	inv := &StateInventory{Container: "https://x/y", Prefix: "prod/"}

	if out := renderStates(t, inv, time.Now()); !strings.Contains(out, "Prefixo:   prod/") {
		t.Errorf("faltou o prefixo no cabeçalho:\n%s", out)
	}
}

func TestWriteStatesReportPropagaErroDeEscrita(t *testing.T) {
	inv := &StateInventory{Container: "https://x/y", States: []StateEntry{{Path: "a.tfstate"}}}

	if err := WriteStatesReport(falhaNoWrite{}, inv, time.Now()); err == nil {
		t.Error("WriteStatesReport não propagou o erro de escrita")
	}
}

func TestHumanSize(t *testing.T) {
	tests := map[int64]string{
		0:          "0 B",
		512:        "512 B",
		1024:       "1.0 KB",
		1536:       "1.5 KB",
		1048576:    "1.0 MB",
		1073741824: "1.0 GB",
	}

	for bytes, want := range tests {
		if got := humanSize(bytes); got != want {
			t.Errorf("humanSize(%d) = %q, quero %q", bytes, got, want)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{20 * time.Second, "0min"},
		{30 * time.Second, "1min"}, // Round arredonda meio minuto para cima
		{5 * time.Minute, "5min"},
		{90 * time.Minute, "1h30min"},
		{3*time.Hour + 12*time.Minute, "3h12min"},
		{26 * time.Hour, "1d02h"},
		{72 * time.Hour, "3d00h"},
	}

	for _, tt := range tests {
		if got := humanDuration(tt.d); got != tt.want {
			t.Errorf("humanDuration(%v) = %q, quero %q", tt.d, got, tt.want)
		}
	}
}
