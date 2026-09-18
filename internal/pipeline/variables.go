package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

// Variable é uma variável de um Variable Group, reduzida ao que é seguro
// exibir.
//
// Value fica vazio a menos que a revelação seja pedida explicitamente, e nunca
// é preenchido para variável secreta — a API do Azure DevOps devolve null no
// valor dessas, então nem chega aqui.
type Variable struct {
	Name     string `json:"name"`
	Secret   bool   `json:"secret"`
	ReadOnly bool   `json:"read_only,omitempty"`
	Value    string `json:"value,omitempty"`
}

// VariableGroupSummary descreve um grupo sem entrar nas variáveis.
type VariableGroupSummary struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Description string    `json:"description,omitempty"`
	Shared      bool      `json:"shared,omitempty"`
	Total       int       `json:"total"`
	Secrets     int       `json:"secrets"`
	ModifiedOn  time.Time `json:"modified_on,omitempty"`
	ModifiedBy  string    `json:"modified_by,omitempty"`
}

// VariableGroupDetail é um grupo com a lista de variáveis.
type VariableGroupDetail struct {
	VariableGroupSummary
	Variables []Variable `json:"variables"`

	// ValuesRevealed diz se os valores não secretos foram incluídos. Vai para
	// o JSON para quem consumir saber o que está olhando.
	ValuesRevealed bool `json:"values_revealed"`
}

// VariableGroupInventory é o resultado da consulta a um projeto.
type VariableGroupInventory struct {
	Project string                 `json:"project"`
	Groups  []VariableGroupSummary `json:"groups"`
}

// ListVariableGroups consulta os grupos do projeto e resume cada um.
func ListVariableGroups(ctx context.Context, lister azure.VariableGroupLister) (*VariableGroupInventory, error) {
	groups, err := lister.ListVariableGroups(ctx)
	if err != nil {
		return nil, err
	}

	inventory := &VariableGroupInventory{Groups: []VariableGroupSummary{}}
	for _, g := range groups {
		inventory.Groups = append(inventory.Groups, summarize(g))
	}

	sort.Slice(inventory.Groups, func(i, j int) bool {
		return strings.ToLower(inventory.Groups[i].Name) < strings.ToLower(inventory.Groups[j].Name)
	})

	return inventory, nil
}

// FindVariableGroup localiza um grupo por nome (sem diferenciar maiúsculas) ou
// por id, e detalha as variáveis.
//
// revealValues inclui os valores das variáveis NÃO secretas. O padrão é não
// revelar: gente guarda segredo em variável comum o tempo todo, e a saída de um
// comando acaba em log de pipeline.
func FindVariableGroup(ctx context.Context, lister azure.VariableGroupLister, nameOrID string, revealValues bool) (*VariableGroupDetail, error) {
	if nameOrID == "" {
		return nil, fmt.Errorf("nome ou id do Variable Group não informado")
	}

	groups, err := lister.ListVariableGroups(ctx)
	if err != nil {
		return nil, err
	}

	for _, g := range groups {
		if !matchesGroup(g, nameOrID) {
			continue
		}
		return detail(g, revealValues), nil
	}

	return nil, fmt.Errorf("Variable Group %q não encontrado no projeto", nameOrID)
}

func matchesGroup(g azure.VariableGroup, nameOrID string) bool {
	if strings.EqualFold(g.Name, nameOrID) {
		return true
	}
	return nameOrID == fmt.Sprint(g.ID)
}

func summarize(g azure.VariableGroup) VariableGroupSummary {
	summary := VariableGroupSummary{
		ID:          g.ID,
		Name:        g.Name,
		Type:        g.Type,
		Description: g.Description,
		Shared:      g.IsShared,
		Total:       len(g.Variables),
		ModifiedOn:  g.ModifiedOn,
		ModifiedBy:  g.ModifiedByName(),
	}

	for _, v := range g.Variables {
		if v.IsSecret {
			summary.Secrets++
		}
	}

	return summary
}

func detail(g azure.VariableGroup, revealValues bool) *VariableGroupDetail {
	d := &VariableGroupDetail{
		VariableGroupSummary: summarize(g),
		Variables:            []Variable{},
		ValuesRevealed:       revealValues,
	}

	for name, v := range g.Variables {
		variable := Variable{
			Name:     name,
			Secret:   v.IsSecret,
			ReadOnly: v.IsReadOnly,
		}
		// Valor só sai com pedido explícito, e nunca de variável secreta —
		// dessas a API devolve null de qualquer forma.
		if revealValues && !v.IsSecret && v.Value != nil {
			variable.Value = *v.Value
		}
		d.Variables = append(d.Variables, variable)
	}

	sort.Slice(d.Variables, func(i, j int) bool {
		return strings.ToLower(d.Variables[i].Name) < strings.ToLower(d.Variables[j].Name)
	})

	return d
}
