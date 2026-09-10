package terraform

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

// lockInfoMetaKey é a chave de metadata onde o backend azurerm guarda o lock,
// em base64. O lock em si é um lease infinito no blob; a metadata só carrega os
// dados de quem travou.
//
// Confirmado contra internal/backend/remote-state/azure/client.go do Terraform.
const lockInfoMetaKey = "terraformlockid"

// stateSuffix é o sufixo dos arquivos de state. Workspaces do backend azurerm
// viram chaves do tipo "prod/app.tfstateenv:staging", por isso a checagem é de
// "contém" e não de sufixo exato.
const stateSuffix = ".tfstate"

// LockState é a situação do lock de um state.
type LockState string

const (
	// LockStateFree: sem lease ativo, ninguém operando.
	LockStateFree LockState = "free"

	// LockStateLocked: lease ativo, alguém está no meio de uma operação — ou
	// ficou preso depois de uma pipeline morrer.
	LockStateLocked LockState = "locked"

	// LockStateStaleMetadata: sem lease, mas com metadata de lock sobrando.
	// O Terraform limpa a metadata ao destravar, então isso costuma ser
	// resquício de execução interrompida. O state não está travado.
	LockStateStaleMetadata LockState = "stale-metadata"
)

// LockInfo espelha os campos que o Terraform grava ao travar um state.
type LockInfo struct {
	ID        string     `json:"id,omitempty"`
	Operation string     `json:"operation,omitempty"`
	Who       string     `json:"who,omitempty"`
	Version   string     `json:"version,omitempty"`
	Created   *time.Time `json:"created,omitempty"`
	Path      string     `json:"path,omitempty"`
}

// StateEntry é um arquivo de state encontrado no container.
type StateEntry struct {
	// Path é a chave do blob, ex.: "prod/app.tfstate".
	Path string `json:"path"`

	// Environment é o primeiro segmento do caminho, que na convenção de pastas
	// por ambiente (dev/hml/prod) é o ambiente. Vazio para state na raiz.
	Environment string `json:"environment,omitempty"`

	Name         string    `json:"name"`
	LastModified time.Time `json:"last_modified"`
	SizeBytes    int64     `json:"size_bytes"`

	Lock LockState `json:"lock"`

	// LeaseState é o estado cru do lease, preservado para diagnóstico de casos
	// que não se resumem a travado/livre (breaking, broken).
	LeaseState string `json:"lease_state,omitempty"`

	// LockInfo traz quem travou e desde quando, se a metadata for legível.
	// É nil quando não há lock ou quando a metadata não pôde ser decodificada.
	LockInfo *LockInfo `json:"lock_info,omitempty"`
}

// Locked informa se o state está travado agora.
func (e StateEntry) Locked() bool {
	return e.Lock == LockStateLocked
}

// LockedFor é há quanto tempo o state está travado. Retorna 0 quando não há
// lock ou quando o horário não veio na metadata.
func (e StateEntry) LockedFor(now time.Time) time.Duration {
	if !e.Locked() || e.LockInfo == nil || e.LockInfo.Created == nil {
		return 0
	}
	return now.Sub(*e.LockInfo.Created)
}

// StateInventory é o resultado da varredura de um container.
type StateInventory struct {
	Container string       `json:"container"`
	Prefix    string       `json:"prefix,omitempty"`
	States    []StateEntry `json:"states"`
}

// Locked devolve só os states travados.
func (i *StateInventory) Locked() []StateEntry {
	var locked []StateEntry
	for _, s := range i.States {
		if s.Locked() {
			locked = append(locked, s)
		}
	}
	return locked
}

// ListStatesOptions ajusta a varredura.
type ListStatesOptions struct {
	// Prefix limita a listagem a um caminho, ex.: "prod/".
	Prefix string

	// IncludeNonState mantém na listagem blobs que não parecem state.
	IncludeNonState bool
}

// ListStates varre o container e classifica os states encontrados.
//
// Somente leitura: lista blobs e lê metadata, nunca baixa nem escreve o state.
func ListStates(ctx context.Context, lister azure.BlobLister, opts ListStatesOptions) (*StateInventory, error) {
	blobs, err := lister.ListBlobs(ctx, opts.Prefix)
	if err != nil {
		return nil, err
	}

	inventory := &StateInventory{
		Prefix: opts.Prefix,
		States: []StateEntry{},
	}

	for _, blob := range blobs {
		if !opts.IncludeNonState && !looksLikeState(blob.Name) {
			continue
		}
		inventory.States = append(inventory.States, newStateEntry(blob))
	}

	// Ordena por ambiente e depois por caminho, que é como a pessoa procura:
	// "o que tem em prod?" antes de "qual arquivo".
	sort.Slice(inventory.States, func(i, j int) bool {
		a, b := inventory.States[i], inventory.States[j]
		if a.Environment != b.Environment {
			return a.Environment < b.Environment
		}
		return a.Path < b.Path
	})

	return inventory, nil
}

func newStateEntry(blob azure.Blob) StateEntry {
	entry := StateEntry{
		Path:         blob.Name,
		Environment:  environmentOf(blob.Name),
		Name:         path.Base(blob.Name),
		LastModified: blob.LastModified,
		SizeBytes:    blob.SizeBytes,
		LeaseState:   blob.LeaseState,
	}

	entry.LockInfo = parseLockInfo(blob.Metadata[lockInfoMetaKey])

	switch {
	case strings.EqualFold(blob.LeaseState, "leased"):
		entry.Lock = LockStateLocked
	case entry.LockInfo != nil:
		entry.Lock = LockStateStaleMetadata
	default:
		entry.Lock = LockStateFree
	}

	return entry
}

// environmentOf extrai o primeiro segmento do caminho. Um state na raiz do
// container não tem ambiente.
func environmentOf(name string) string {
	dir, _, found := strings.Cut(name, "/")
	if !found {
		return ""
	}
	return dir
}

func looksLikeState(name string) bool {
	return strings.Contains(strings.ToLower(name), stateSuffix)
}

// parseLockInfo decodifica a metadata de lock. Metadata ilegível vira nil em
// vez de erro: uma entrada corrompida não pode derrubar a listagem inteira, e o
// estado do lease continua sendo a fonte de verdade sobre estar travado.
func parseLockInfo(encoded string) *LockInfo {
	if encoded == "" {
		return nil
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}

	var info LockInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil
	}
	if info == (LockInfo{}) {
		return nil
	}
	return &info
}
