package pipeline

import (
	"regexp"
	"sort"
	"strings"
)

// subscriptionGUID acha o GUID de assinatura que o Azure DevOps coloca no nome
// sugerido da conexão, ex.: "MPN - FULANO - (00000000-0000-0000-0000-000000000000)".
var subscriptionGUID = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// DuplicateGroup é um conjunto de conexões do mesmo tipo cujo nome aponta para
// a mesma assinatura.
//
// É candidato, não veredito: duas conexões para a mesma assinatura podem
// existir de propósito, com escopos ou permissões diferentes. O que os casos
// reais mostraram foi outra coisa — pares que diferem só por grafia
// ("Devios"/"Devops", "ACR MPN"/"ACR-MPN") —, e cada par desses é um segredo a
// mais para renovar.
type DuplicateGroup struct {
	Type         string   `json:"type"`
	Subscription string   `json:"subscription"`
	Names        []string `json:"names"`
}

// findDuplicates agrupa por tipo e GUID de assinatura no nome.
//
// O tipo entra na chave porque uma conexão de registry e uma azurerm para a
// mesma assinatura são coisas diferentes. Conexão sem GUID no nome fica de
// fora: comparar nome livre por semelhança seria chute.
func findDuplicates(connections []Connection) []DuplicateGroup {
	type key struct{ typ, sub string }

	groups := map[key][]string{}
	for _, c := range connections {
		guid := subscriptionGUID.FindString(c.Name)
		if guid == "" {
			continue
		}
		k := key{strings.ToLower(c.Type), strings.ToLower(guid)}
		groups[k] = append(groups[k], c.Name)
	}

	out := []DuplicateGroup{}
	for k, names := range groups {
		if len(names) < 2 {
			continue
		}
		sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
		out = append(out, DuplicateGroup{Type: k.typ, Subscription: k.sub, Names: names})
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Names[0]) < strings.ToLower(out[j].Names[0])
	})
	return out
}
