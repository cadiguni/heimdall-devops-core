package terraform

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

// fakeLister devolve blobs fixos, para exercitar a classificação sem Azure.
type fakeLister struct {
	blobs      []azure.Blob
	err        error
	gotPrefix  string
	callCount  int
	prefixSeen bool
}

func (f *fakeLister) ListBlobs(_ context.Context, prefix string) ([]azure.Blob, error) {
	f.callCount++
	f.gotPrefix = prefix
	f.prefixSeen = true
	if f.err != nil {
		return nil, f.err
	}
	return f.blobs, nil
}

// lockMetadata monta a metadata como o backend azurerm grava: JSON do LockInfo
// em base64. As chaves vêm capitalizadas porque a struct do Terraform não tem
// tags de json.
func lockMetadata(t *testing.T, jsonBody string) map[string]string {
	t.Helper()
	return map[string]string{
		lockInfoMetaKey: base64.StdEncoding.EncodeToString([]byte(jsonBody)),
	}
}

const lockBody = `{"ID":"4a1e6bd4-2c7f-4a1b-9f3e-2b7c5d8e1a90",` +
	`"Operation":"OperationTypePlan","Info":"","Who":"lucas@vm-build",` +
	`"Version":"1.14.5","Created":"2026-09-10T11:00:00Z","Path":"time1/prod/app.tfstate"}`

func listStates(t *testing.T, lister azure.BlobLister, opts ListStatesOptions) *StateInventory {
	t.Helper()

	inv, err := ListStates(context.Background(), lister, opts)
	if err != nil {
		t.Fatalf("ListStates: %v", err)
	}
	return inv
}

func TestListStatesClassificaLock(t *testing.T) {
	lister := &fakeLister{blobs: []azure.Blob{
		{Name: "dev/app.tfstate", LeaseState: "available"},
		{Name: "prod/app.tfstate", LeaseState: "leased", Metadata: lockMetadata(t, lockBody)},
		{Name: "hml/app.tfstate", LeaseState: "leased"},
		{Name: "hml/orfao.tfstate", LeaseState: "available", Metadata: lockMetadata(t, lockBody)},
	}}

	inv := listStates(t, lister, ListStatesOptions{})

	want := map[string]LockState{
		"dev/app.tfstate":   LockStateFree,
		"prod/app.tfstate":  LockStateLocked,
		"hml/app.tfstate":   LockStateLocked,
		"hml/orfao.tfstate": LockStateStaleMetadata,
	}

	if len(inv.States) != len(want) {
		t.Fatalf("states = %d, quero %d", len(inv.States), len(want))
	}
	for _, s := range inv.States {
		if got := s.Lock; got != want[s.Path] {
			t.Errorf("%s: lock = %q, quero %q", s.Path, got, want[s.Path])
		}
	}

	if locked := inv.Locked(); len(locked) != 2 {
		t.Errorf("Locked() = %d, quero 2", len(locked))
	}
}

func TestListStatesLeLockInfo(t *testing.T) {
	lister := &fakeLister{blobs: []azure.Blob{
		{Name: "prod/app.tfstate", LeaseState: "leased", Metadata: lockMetadata(t, lockBody)},
	}}

	s := listStates(t, lister, ListStatesOptions{}).States[0]

	if s.LockInfo == nil {
		t.Fatal("LockInfo = nil, quero os dados do lock")
	}
	if s.LockInfo.Who != "lucas@vm-build" {
		t.Errorf("Who = %q", s.LockInfo.Who)
	}
	if s.LockInfo.Operation != "OperationTypePlan" {
		t.Errorf("Operation = %q", s.LockInfo.Operation)
	}
	if s.LockInfo.ID != "4a1e6bd4-2c7f-4a1b-9f3e-2b7c5d8e1a90" {
		t.Errorf("ID = %q", s.LockInfo.ID)
	}
	if s.LockInfo.Created == nil {
		t.Fatal("Created = nil")
	}

	// Lease ativo sem metadata legível continua sendo lock.
	semMeta := &fakeLister{blobs: []azure.Blob{{Name: "a.tfstate", LeaseState: "leased"}}}
	got := listStates(t, semMeta, ListStatesOptions{}).States[0]
	if !got.Locked() || got.LockInfo != nil {
		t.Errorf("lease sem metadata: Locked=%v LockInfo=%v", got.Locked(), got.LockInfo)
	}
}

func TestLockedFor(t *testing.T) {
	criado := time.Date(2026, 9, 10, 11, 0, 0, 0, time.UTC)
	agora := criado.Add(3*time.Hour + 12*time.Minute)

	travado := StateEntry{Lock: LockStateLocked, LockInfo: &LockInfo{Created: &criado}}
	if got := travado.LockedFor(agora); got != 3*time.Hour+12*time.Minute {
		t.Errorf("LockedFor = %v, quero 3h12m", got)
	}

	// Sem lock, sem LockInfo ou sem Created: zero, nunca um valor inventado.
	for name, entry := range map[string]StateEntry{
		"sem lock":     {Lock: LockStateFree, LockInfo: &LockInfo{Created: &criado}},
		"sem lockinfo": {Lock: LockStateLocked},
		"sem created":  {Lock: LockStateLocked, LockInfo: &LockInfo{}},
	} {
		if got := entry.LockedFor(agora); got != 0 {
			t.Errorf("%s: LockedFor = %v, quero 0", name, got)
		}
	}
}

func TestListStatesFiltraBlobsQueNaoSaoState(t *testing.T) {
	blobs := []azure.Blob{
		{Name: "prod/app.tfstate"},
		{Name: "prod/app.tfstateenv:staging"}, // workspace do backend azurerm
		{Name: "prod/README.md"},
		{Name: "prod/backup.zip"},
	}

	inv := listStates(t, &fakeLister{blobs: blobs}, ListStatesOptions{})
	if len(inv.States) != 2 {
		t.Errorf("states = %d, quero 2 (só os .tfstate): %+v", len(inv.States), inv.States)
	}

	todos := listStates(t, &fakeLister{blobs: blobs}, ListStatesOptions{IncludeNonState: true})
	if len(todos.States) != 4 {
		t.Errorf("com --all: states = %d, quero 4", len(todos.States))
	}
}

func TestListStatesOrdenaPorAmbienteEDepoisCaminho(t *testing.T) {
	lister := &fakeLister{blobs: []azure.Blob{
		{Name: "prod/z.tfstate"},
		{Name: "dev/b.tfstate"},
		{Name: "raiz.tfstate"},
		{Name: "prod/a.tfstate"},
		{Name: "dev/a.tfstate"},
	}}

	inv := listStates(t, lister, ListStatesOptions{})

	want := []string{"raiz.tfstate", "dev/a.tfstate", "dev/b.tfstate", "prod/a.tfstate", "prod/z.tfstate"}
	for i, w := range want {
		if inv.States[i].Path != w {
			t.Errorf("posição %d = %q, quero %q", i, inv.States[i].Path, w)
		}
	}
}

func TestListStatesPreenchePathEAmbiente(t *testing.T) {
	lister := &fakeLister{blobs: []azure.Blob{
		{Name: "prod/subpasta/app.tfstate", SizeBytes: 2048},
		{Name: "raiz.tfstate"},
	}}

	inv := listStates(t, lister, ListStatesOptions{})

	aninhado := inv.States[1]
	if aninhado.Environment != "prod" || aninhado.Name != "app.tfstate" {
		t.Errorf("aninhado = env %q nome %q", aninhado.Environment, aninhado.Name)
	}
	if aninhado.SizeBytes != 2048 {
		t.Errorf("SizeBytes = %d", aninhado.SizeBytes)
	}

	raiz := inv.States[0]
	if raiz.Environment != "" {
		t.Errorf("state na raiz não deveria ter ambiente, tem %q", raiz.Environment)
	}
}

func TestListStatesRepassaPrefixo(t *testing.T) {
	lister := &fakeLister{}

	inv := listStates(t, lister, ListStatesOptions{Prefix: "prod/"})

	if lister.gotPrefix != "prod/" {
		t.Errorf("prefixo repassado = %q, quero %q", lister.gotPrefix, "prod/")
	}
	if inv.Prefix != "prod/" {
		t.Errorf("inv.Prefix = %q", inv.Prefix)
	}
	// Sem resultados o campo precisa serializar como [] e não null.
	if inv.States == nil {
		t.Error("States = nil, quero slice vazio")
	}
}

func TestListStatesPropagaErro(t *testing.T) {
	falha := errors.New("403 ao listar")

	_, err := ListStates(context.Background(), &fakeLister{err: falha}, ListStatesOptions{})
	if !errors.Is(err, falha) {
		t.Errorf("erro = %v, quero %v", err, falha)
	}
}

// Metadata ilegível não pode derrubar a listagem: o lease continua sendo a
// fonte de verdade sobre estar travado.
func TestParseLockInfoEntradaInvalida(t *testing.T) {
	tests := map[string]string{
		"vazia":            "",
		"base64 inválido":  "não é base64!!",
		"json inválido":    base64.StdEncoding.EncodeToString([]byte("{isso não é json")),
		"json sem campos":  base64.StdEncoding.EncodeToString([]byte(`{}`)),
		"json de outra co": base64.StdEncoding.EncodeToString([]byte(`{"outro":"campo"}`)),
	}

	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			if got := parseLockInfo(encoded); got != nil {
				t.Errorf("parseLockInfo = %+v, quero nil", got)
			}
		})
	}
}

func TestListStatesMetadataCorrompidaNaoDerrubaListagem(t *testing.T) {
	lister := &fakeLister{blobs: []azure.Blob{
		{Name: "prod/app.tfstate", LeaseState: "leased", Metadata: map[string]string{lockInfoMetaKey: "lixo!!"}},
	}}

	inv := listStates(t, lister, ListStatesOptions{})

	if len(inv.States) != 1 {
		t.Fatalf("states = %d, quero 1", len(inv.States))
	}
	if !inv.States[0].Locked() {
		t.Error("lease ativo precisa continuar contando como travado")
	}
	if inv.States[0].LockInfo != nil {
		t.Error("metadata corrompida deveria virar LockInfo nil")
	}
}
