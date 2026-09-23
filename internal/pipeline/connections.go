package pipeline

import (
	"context"
	"sort"
	"strings"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

// CredentialLifetime classifica se a credencial por trás da conexão vence.
//
// A data em si não existe na API do Azure DevOps — o segredo vive no Entra ID
// ou no provedor externo. O que dá para responder aqui é a pergunta anterior e
// mais útil: *esta conexão depende de algo que vence?*
type CredentialLifetime string

const (
	// LifetimeExpires: depende de segredo com validade (client secret,
	// certificado, PAT). Vence um dia, e a pipeline quebra quando vencer.
	LifetimeExpires CredentialLifetime = "expires"

	// LifetimePermanent: federação de identidade de carga de trabalho ou
	// identidade gerenciada. Não há segredo para vencer.
	LifetimePermanent CredentialLifetime = "permanent"

	// LifetimeUnknown: esquema cuja validade depende do provedor externo e não
	// dá para afirmar daqui.
	LifetimeUnknown CredentialLifetime = "unknown"
)

// lifetimeByScheme mapeia o esquema de autenticação da conexão.
//
// Os nomes são os que o Azure DevOps usa no campo authorization.scheme.
var lifetimeByScheme = map[string]CredentialLifetime{
	"workloadidentityfederation": LifetimePermanent,
	"managedserviceidentity":     LifetimePermanent,
	"none":                       LifetimePermanent,

	"serviceprincipal":    LifetimeExpires,
	"certificate":         LifetimeExpires,
	"token":               LifetimeExpires,
	"personalaccesstoken": LifetimeExpires,
}

// Connection é uma service connection, reduzida ao que interessa.
type Connection struct {
	Name string `json:"name"`
	Type string `json:"type"`

	// Scheme é o esquema de autenticação cru, preservado para diagnóstico de
	// esquemas que este código ainda não classifica.
	Scheme string `json:"scheme"`

	Lifetime CredentialLifetime `json:"lifetime"`

	Ready     bool   `json:"ready"`
	Shared    bool   `json:"shared,omitempty"`
	CreatedBy string `json:"created_by,omitempty"`
}

// ConnectionInventory é o resultado da consulta a um projeto.
type ConnectionInventory struct {
	Project     string       `json:"project"`
	Connections []Connection `json:"connections"`
}

// Expiring devolve as conexões que dependem de segredo com validade.
func (i *ConnectionInventory) Expiring() []Connection {
	return i.filter(func(c Connection) bool { return c.Lifetime == LifetimeExpires })
}

// NotReady devolve as conexões que o Azure DevOps não considera prontas —
// normalmente credencial inválida ou criação que falhou pela metade.
func (i *ConnectionInventory) NotReady() []Connection {
	return i.filter(func(c Connection) bool { return !c.Ready })
}

func (i *ConnectionInventory) filter(keep func(Connection) bool) []Connection {
	var out []Connection
	for _, c := range i.Connections {
		if keep(c) {
			out = append(out, c)
		}
	}
	return out
}

// ListConnections consulta as service connections e classifica cada uma.
func ListConnections(ctx context.Context, lister azure.ServiceEndpointLister) (*ConnectionInventory, error) {
	endpoints, err := lister.ListServiceEndpoints(ctx)
	if err != nil {
		return nil, err
	}

	inventory := &ConnectionInventory{Connections: []Connection{}}
	for _, e := range endpoints {
		inventory.Connections = append(inventory.Connections, Connection{
			Name:      e.Name,
			Type:      e.Type,
			Scheme:    e.Authorization.Scheme,
			Lifetime:  lifetimeOf(e.Authorization.Scheme),
			Ready:     e.IsReady,
			Shared:    e.IsShared,
			CreatedBy: e.CreatedBy.DisplayName,
		})
	}

	sort.Slice(inventory.Connections, func(i, j int) bool {
		return strings.ToLower(inventory.Connections[i].Name) < strings.ToLower(inventory.Connections[j].Name)
	})

	return inventory, nil
}

// lifetimeOf classifica o esquema. Esquema desconhecido vira "unknown" em vez
// de ser chutado para um dos lados: dizer que algo não expira quando expira
// seria o pior erro possível aqui.
func lifetimeOf(scheme string) CredentialLifetime {
	if lifetime, ok := lifetimeByScheme[strings.ToLower(scheme)]; ok {
		return lifetime
	}
	return LifetimeUnknown
}
