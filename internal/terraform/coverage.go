package terraform

import (
	"context"
	"fmt"
	"github.com/cadiguni/heimdall-devops-core/internal/azure"
	"sort"
	"strings"
)

// CoverageStatus diz como um resource group se relaciona com os states
// varridos. Os valores são identificadores estáveis do JSON.
type CoverageStatus string

const (
	// CoverageTracked: o resource group existe na assinatura e algum state o
	// rastreia.
	CoverageTracked CoverageStatus = "tracked"
	// CoverageUntracked: existe na assinatura e nenhum state varrido o rastreia.
	CoverageUntracked CoverageStatus = "untracked"
	// CoverageMissing: algum state o rastreia, mas ele não está na assinatura.
	CoverageMissing CoverageStatus = "missing"
)

// StateTracking é o que um state diz sobre resource groups, reduzido ao
// necessário: nenhum atributo do state entra aqui.
type StateTracking struct {
	Path string `json:"path"`
	// Groups mapeia o GUID da assinatura (minúsculo) aos resource groups que o
	// state rastreia nela.
	Groups map[string][]string `json:"groups,omitempty"`
	// Err é preenchido quando o state não pôde ser lido. State ilegível não é
	// state vazio.
	Err string `json:"error,omitempty"`
}

// TrackingOf extrai de uma lista de recursos os resource groups rastreados.
// Data source é ignorado: é relido a cada plan e não prova que o grupo tem
// state. ID que não carrega assinatura e resource group também é ignorado.
func TrackingOf(path string, resources []StateResource) StateTracking {
	t := StateTracking{Path: path, Groups: map[string][]string{}}
	seen := map[string]bool{}

	for _, r := range resources {
		if r.Mode == "data" {
			continue
		}
		sub, group, ok := parseResourceGroupID(r.ID)
		if !ok {
			continue
		}
		key := sub + "/" + strings.ToLower(group)
		if seen[key] {
			continue
		}
		seen[key] = true
		t.Groups[sub] = append(t.Groups[sub], group)
	}

	for _, g := range t.Groups {
		sort.Strings(g)
	}
	return t
}

// parseResourceGroupID tira assinatura e resource group de um ID do ARM.
func parseResourceGroupID(id string) (sub, group string, ok bool) {
	parts := strings.Split(strings.Trim(id, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		switch strings.ToLower(parts[i]) {
		case "subscriptions":
			if sub == "" {
				sub = strings.ToLower(parts[i+1])
			}
		case "resourcegroups":
			if group == "" {
				group = parts[i+1]
			}
		}
	}
	if !strings.HasPrefix(strings.ToLower(id), "/subscriptions/") || sub == "" || group == "" {
		return "", "", false
	}
	return sub, group, true
}

// CollectTracking baixa e resume cada state. Falha em um state não derruba a
// varredura: vira StateTracking.Err.
func CollectTracking(ctx context.Context, client StateClient, paths []string) []StateTracking {
	out := make([]StateTracking, 0, len(paths))
	for _, p := range paths {
		inspection, err := InspectStateBlob(ctx, client, p)
		if err != nil {
			out = append(out, StateTracking{Path: p, Err: err.Error()})
			continue
		}
		out = append(out, TrackingOf(p, inspection.Resources))
	}
	return out
}

// CoverageEntry é a situação de um resource group.
type CoverageEntry struct {
	ResourceGroup string            `json:"resource_group"`
	Status        CoverageStatus    `json:"status"`
	Location      string            `json:"location,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	States        []string          `json:"states,omitempty"`
}

// CoverageReport cruza os resource groups de uma assinatura com os states de
// um container.
type CoverageReport struct {
	Subscription  string          `json:"subscription"`
	Container     string          `json:"container"`
	ScannedGroups int             `json:"scanned_groups"`
	ScannedStates int             `json:"scanned_states"`
	Entries       []CoverageEntry `json:"entries"`
	Notes         []string        `json:"notes,omitempty"`
}

// ByStatus devolve as entradas de um status, na ordem do relatório.
func (r *CoverageReport) ByStatus(s CoverageStatus) []CoverageEntry {
	var out []CoverageEntry
	for _, e := range r.Entries {
		if e.Status == s {
			out = append(out, e)
		}
	}
	return out
}

// BuildCoverage cruza grupos e states. Função pura: só conta como cobertura o
// state que fala da assinatura pedida, e o nome do grupo é comparado sem
// diferenciar caixa.
func BuildCoverage(subscription string, groups []azure.ResourceGroup, states []StateTracking) *CoverageReport {
	sub := strings.ToLower(subscription)
	r := &CoverageReport{
		Subscription:  subscription,
		ScannedGroups: len(groups),
		Entries:       []CoverageEntry{},
	}

	byGroup := map[string][]string{}
	display := map[string]string{}
	for _, st := range states {
		if st.Err != "" {
			r.Notes = append(r.Notes, fmt.Sprintf("%s não foi lido (%s); os resource groups dele não entraram na conta", st.Path, st.Err))
			continue
		}
		r.ScannedStates++
		for _, g := range st.Groups[sub] {
			k := strings.ToLower(g)
			byGroup[k] = append(byGroup[k], st.Path)
			if _, ok := display[k]; !ok {
				display[k] = g
			}
		}
	}
	for k := range byGroup {
		sort.Strings(byGroup[k])
	}

	exists := map[string]bool{}
	for _, g := range groups {
		k := strings.ToLower(g.Name)
		exists[k] = true
		e := CoverageEntry{ResourceGroup: g.Name, Location: g.Location, Tags: g.Tags}
		if sts := byGroup[k]; len(sts) > 0 {
			e.Status, e.States = CoverageTracked, sts
		} else {
			e.Status = CoverageUntracked
		}
		r.Entries = append(r.Entries, e)
	}

	for k, sts := range byGroup {
		if !exists[k] {
			r.Entries = append(r.Entries, CoverageEntry{
				ResourceGroup: display[k], Status: CoverageMissing, States: sts,
			})
		}
	}

	sort.SliceStable(r.Entries, func(i, j int) bool {
		return strings.ToLower(r.Entries[i].ResourceGroup) < strings.ToLower(r.Entries[j].ResourceGroup)
	})
	return r
}
