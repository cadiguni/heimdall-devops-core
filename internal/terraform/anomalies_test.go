package terraform

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

// Os casos abaixo são os que apareceram de verdade no container inspecionado.
func TestDetectAnomalies(t *testing.T) {
	tests := []struct {
		name  string
		entry StateEntry
		want  []AnomalyKind
	}{
		{
			name:  "variável do Azure DevOps não substituída",
			entry: StateEntry{Path: "dev/gvhublayoutfrontapi$(AliasAssinatura).tfstate", SizeBytes: 180},
			want:  []AnomalyKind{AnomalyUnexpandedVariable, AnomalyPossiblyEmpty},
		},
		{
			name:  "interpolação de shell não substituída",
			entry: StateEntry{Path: "dev/app${AMBIENTE}.tfstate", SizeBytes: 5000},
			want:  []AnomalyKind{AnomalyUnexpandedVariable},
		},
		{
			name:  "key= vazou para o caminho",
			entry: StateEntry{Path: "key=dev/gvpay.tfstate", SizeBytes: 2500},
			want:  []AnomalyKind{AnomalyBackendKeyPrefix},
		},
		{
			name:  "hífen solto antes da extensão",
			entry: StateEntry{Path: "dev/elkobservability-.tfstate", SizeBytes: 5000},
			want:  []AnomalyKind{AnomalyDanglingSeparator},
		},
		{
			name:  "underscore solto antes da extensão",
			entry: StateEntry{Path: "dev/app_.tfstate", SizeBytes: 5000},
			want:  []AnomalyKind{AnomalyDanglingSeparator},
		},
		{
			name:  "tamanho de state vazio",
			entry: StateEntry{Path: "prod/redis.tfstate", SizeBytes: 180},
			want:  []AnomalyKind{AnomalyPossiblyEmpty},
		},
		{
			name:  "state normal",
			entry: StateEntry{Path: "prod/app.tfstate", SizeBytes: 85565},
			want:  nil,
		},
		{
			name:  "state pequeno mas acima do limite",
			entry: StateEntry{Path: "dev/iagateway.tfstate", SizeBytes: 445},
			want:  nil,
		},
		{
			name:  "tamanho zero não é suspeita de vazio",
			entry: StateEntry{Path: "dev/app.tfstate", SizeBytes: 0},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectAnomalies(tt.entry)

			if len(got) != len(tt.want) {
				t.Fatalf("anomalias = %+v, quero %v", got, tt.want)
			}
			for i, kind := range tt.want {
				if got[i].Kind != kind {
					t.Errorf("posição %d = %q, quero %q", i, got[i].Kind, kind)
				}
				if got[i].Detail == "" {
					t.Errorf("%s: detalhe vazio", kind)
				}
			}
		})
	}
}

// O limite de 300 bytes precisa ficar acima de um state vazio real (181 bytes,
// medido) e abaixo de qualquer state com recurso.
func TestLimiteDeStateVazio(t *testing.T) {
	const stateVazioReal = 181

	if emptyStateBytes <= stateVazioReal {
		t.Errorf("limite %d não pega um state vazio real de %d bytes", emptyStateBytes, stateVazioReal)
	}
	if emptyStateBytes >= 1000 {
		t.Errorf("limite %d está alto demais, pegaria state com recursos", emptyStateBytes)
	}
}

func TestListStatesPreencheAnomalias(t *testing.T) {
	lister := &fakeLister{blobs: []azure.Blob{
		{Name: "key=dev/gvpay.tfstate", SizeBytes: 2500},
		{Name: "prod/app.tfstate", SizeBytes: 85565},
	}}

	inv := listStates(t, lister, ListStatesOptions{})

	anomalos := inv.Anomalous()
	if len(anomalos) != 1 {
		t.Fatalf("anômalos = %d, quero 1: %+v", len(anomalos), anomalos)
	}
	if anomalos[0].Path != "key=dev/gvpay.tfstate" {
		t.Errorf("anômalo = %q", anomalos[0].Path)
	}
}

func TestWriteStatesReportListaAnomalias(t *testing.T) {
	inv := &StateInventory{
		Container: "https://x/y",
		States: []StateEntry{
			{
				Path: "prod/redis.tfstate", Lock: LockStateFree, SizeBytes: 180,
				Anomalies: []Anomaly{{Kind: AnomalyPossiblyEmpty, Detail: "180 bytes, compatível com state sem recursos"}},
			},
		},
	}

	var buf bytes.Buffer
	if err := WriteStatesReport(&buf, inv, time.Now()); err != nil {
		t.Fatalf("WriteStatesReport: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"States com irregularidade (1)",
		"prod/redis.tfstate",
		"[possibly-empty]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q:\n%s", want, out)
		}
	}
}

func TestWriteStatesReportSemAnomalias(t *testing.T) {
	inv := &StateInventory{
		Container: "https://x/y",
		States:    []StateEntry{{Path: "prod/app.tfstate", Lock: LockStateFree, SizeBytes: 85565}},
	}

	var buf bytes.Buffer
	if err := WriteStatesReport(&buf, inv, time.Now()); err != nil {
		t.Fatalf("WriteStatesReport: %v", err)
	}

	if strings.Contains(buf.String(), "irregularidade") {
		t.Errorf("seção de anomalias apareceu sem anomalia:\n%s", buf.String())
	}
}
