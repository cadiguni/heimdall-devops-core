package pipeline

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

type fakeEndpointLister struct {
	endpoints []azure.ServiceEndpoint
	err       error
}

func (f *fakeEndpointLister) ListServiceEndpoints(context.Context) ([]azure.ServiceEndpoint, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.endpoints, nil
}

func conexao(nome, tipo, scheme string, pronta bool) azure.ServiceEndpoint {
	return azure.ServiceEndpoint{
		Name:          nome,
		Type:          tipo,
		IsReady:       pronta,
		Authorization: azure.EndpointAuthorization{Scheme: scheme},
		CreatedBy:     azure.IdentityRef{DisplayName: "Fulano"},
	}
}

func conexoesDeExemplo() []azure.ServiceEndpoint {
	return []azure.ServiceEndpoint{
		conexao("zeta-federada", "azurerm", "WorkloadIdentityFederation", true),
		conexao("alfa-com-secret", "azurerm", "ServicePrincipal", true),
		conexao("docker-hub", "dockerregistry", "UsernamePassword", true),
		conexao("github-token", "github", "PersonalAccessToken", true),
		conexao("quebrada", "azurerm", "ServicePrincipal", false),
	}
}

func listarConexoes(t *testing.T, lister azure.ServiceEndpointLister) *ConnectionInventory {
	t.Helper()

	inv, err := ListConnections(context.Background(), lister)
	if err != nil {
		t.Fatalf("ListConnections: %v", err)
	}
	return inv
}

func TestListConnectionsClassificaValidade(t *testing.T) {
	inv := listarConexoes(t, &fakeEndpointLister{endpoints: conexoesDeExemplo()})

	want := map[string]CredentialLifetime{
		"zeta-federada":   LifetimePermanent,
		"alfa-com-secret": LifetimeExpires,
		"github-token":    LifetimeExpires,
		"quebrada":        LifetimeExpires,
		// UsernamePassword depende do provedor externo: não dá para afirmar.
		"docker-hub": LifetimeUnknown,
	}

	for _, c := range inv.Connections {
		if got := c.Lifetime; got != want[c.Name] {
			t.Errorf("%s: validade = %q, quero %q", c.Name, got, want[c.Name])
		}
	}

	if len(inv.Expiring()) != 3 {
		t.Errorf("que vencem = %d, quero 3", len(inv.Expiring()))
	}
}

// Esquema desconhecido não pode ser chutado para "não vence": dizer que algo é
// permanente quando não é seria o pior erro possível aqui.
func TestEsquemaDesconhecidoNaoViraPermanente(t *testing.T) {
	inv := listarConexoes(t, &fakeEndpointLister{
		endpoints: []azure.ServiceEndpoint{conexao("nova", "algo", "EsquemaQueNaoExiste", true)},
	})

	if got := inv.Connections[0].Lifetime; got != LifetimeUnknown {
		t.Errorf("validade = %q, quero unknown", got)
	}
	// E o esquema cru fica visível, para dar o que investigar.
	if inv.Connections[0].Scheme != "EsquemaQueNaoExiste" {
		t.Errorf("Scheme = %q", inv.Connections[0].Scheme)
	}
}

func TestListConnectionsClassificaSemDiferenciarCaixa(t *testing.T) {
	inv := listarConexoes(t, &fakeEndpointLister{
		endpoints: []azure.ServiceEndpoint{conexao("x", "azurerm", "workloadidentityfederation", true)},
	})

	if got := inv.Connections[0].Lifetime; got != LifetimePermanent {
		t.Errorf("validade = %q, quero permanent", got)
	}
}

func TestListConnectionsOrdenaEApontaQuebradas(t *testing.T) {
	inv := listarConexoes(t, &fakeEndpointLister{endpoints: conexoesDeExemplo()})

	if inv.Connections[0].Name != "alfa-com-secret" {
		t.Errorf("primeira = %q, esperava ordem alfabética", inv.Connections[0].Name)
	}

	quebradas := inv.NotReady()
	if len(quebradas) != 1 || quebradas[0].Name != "quebrada" {
		t.Errorf("não prontas = %+v", quebradas)
	}
}

func TestListConnectionsVazio(t *testing.T) {
	inv := listarConexoes(t, &fakeEndpointLister{})

	if inv.Connections == nil {
		t.Error("Connections = nil, quero slice vazio")
	}
	if len(inv.Expiring()) != 0 || len(inv.NotReady()) != 0 {
		t.Error("projeto sem conexões não deveria ter achado nenhum")
	}
}

func TestListConnectionsPropagaErro(t *testing.T) {
	falha := errors.New("403 sem permissão")

	_, err := ListConnections(context.Background(), &fakeEndpointLister{err: falha})
	if !errors.Is(err, falha) {
		t.Errorf("erro = %v, quero %v", err, falha)
	}
}

func renderConexoes(t *testing.T, endpoints []azure.ServiceEndpoint) string {
	t.Helper()

	inv := listarConexoes(t, &fakeEndpointLister{endpoints: endpoints})
	inv.Project = "https://dev.azure.com/minhaorg/MeuProjeto"

	var buf bytes.Buffer
	if err := WriteConnections(&buf, inv); err != nil {
		t.Fatalf("WriteConnections: %v", err)
	}
	return buf.String()
}

func TestWriteConnections(t *testing.T) {
	out := renderConexoes(t, conexoesDeExemplo())

	for _, want := range []string{
		"https://dev.azure.com/minhaorg/MeuProjeto",
		"alfa-com-secret",
		"zeta-federada",
		"não vence",
		"Dependem de segredo com validade (3)",
		"não considera prontas (1)",
		// A ausência da data precisa ser explicada, não omitida.
		"não está na API do Azure DevOps",
		// E a saída precisa apontar a solução, não só o problema.
		"federação de identidade",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q:\n%s", want, out)
		}
	}
}

func TestWriteConnectionsTudoFederado(t *testing.T) {
	out := renderConexoes(t, []azure.ServiceEndpoint{
		conexao("a", "azurerm", "WorkloadIdentityFederation", true),
	})

	if !strings.Contains(out, "Nenhuma conexão depende de segredo com validade.") {
		t.Errorf("faltou a confirmação de que nada vence:\n%s", out)
	}
	if strings.Contains(out, "não considera prontas") {
		t.Errorf("sem conexão quebrada não deveria ter a seção:\n%s", out)
	}
}

func TestWriteConnectionsVazio(t *testing.T) {
	var buf bytes.Buffer
	inv := &ConnectionInventory{Project: "https://x/y", Connections: []Connection{}}

	if err := WriteConnections(&buf, inv); err != nil {
		t.Fatalf("WriteConnections: %v", err)
	}
	if !strings.Contains(buf.String(), "Nenhuma service connection encontrada.") {
		t.Errorf("faltou a mensagem de projeto sem conexões:\n%s", buf.String())
	}
}

func TestWriteConnectionsPropagaErroDeEscrita(t *testing.T) {
	inv := listarConexoes(t, &fakeEndpointLister{endpoints: conexoesDeExemplo()})

	if err := WriteConnections(escritorQueFalha{}, inv); err == nil {
		t.Error("WriteConnections não propagou o erro de escrita")
	}
}

const (
	assinaturaA = "11111111-aaaa-4bbb-8ccc-000000000001"
	assinaturaB = "22222222-aaaa-4bbb-8ccc-000000000002"
)

// Os três pares vêm da listagem real de 61 conexões, com GUID trocado: dois
// diferem só por grafia, e um por um sufixo.
func conexoesComDuplicata() []azure.ServiceEndpoint {
	return []azure.ServiceEndpoint{
		conexao("DeviosACR - MPN - FULANO - SB ("+assinaturaA+")", "dockerregistry", "ServicePrincipal", true),
		conexao("DevopsACR - MPN - FULANO - SB ("+assinaturaA+")", "dockerregistry", "ServicePrincipal", true),
		conexao("MPN - CICRANO - ("+assinaturaB+")", "azurerm", "ServicePrincipal", true),
		// GUID em maiúscula: o mesmo GUID, grafado diferente.
		conexao("MPN - CICRANO - NAP ("+strings.ToUpper(assinaturaB)+")", "azurerm", "ServicePrincipal", true),
		// Mesma assinatura do par ACR, mas outro tipo: não é duplicata.
		conexao("MPN - FULANO - ("+assinaturaA+")", "azurerm", "ServicePrincipal", true),
		// Sem GUID no nome: fica de fora, mesmo com nome parecido.
		conexao("docker-hub", "dockerregistry", "UsernamePassword", true),
		conexao("docker hub", "dockerregistry", "UsernamePassword", true),
	}
}

func TestListConnectionsApontaDuplicatas(t *testing.T) {
	inv := listarConexoes(t, &fakeEndpointLister{endpoints: conexoesComDuplicata()})

	if len(inv.Duplicates) != 2 {
		t.Fatalf("grupos = %d, quero 2: %+v", len(inv.Duplicates), inv.Duplicates)
	}

	acr := inv.Duplicates[0]
	if acr.Type != "dockerregistry" || acr.Subscription != assinaturaA || len(acr.Names) != 2 {
		t.Errorf("primeiro grupo = %+v", acr)
	}

	mpn := inv.Duplicates[1]
	if mpn.Type != "azurerm" || mpn.Subscription != assinaturaB || len(mpn.Names) != 2 {
		t.Errorf("segundo grupo = %+v", mpn)
	}
	for _, name := range mpn.Names {
		if strings.HasPrefix(name, "MPN - FULANO") {
			t.Errorf("conexão de outra assinatura entrou no grupo: %q", name)
		}
	}
}

func TestListConnectionsSemDuplicataDevolveListaVazia(t *testing.T) {
	inv := listarConexoes(t, &fakeEndpointLister{endpoints: conexoesDeExemplo()})

	if inv.Duplicates == nil || len(inv.Duplicates) != 0 {
		t.Errorf("Duplicates = %#v, quero slice vazio", inv.Duplicates)
	}
}

func TestWriteConnectionsDuplicatas(t *testing.T) {
	out := renderConexoes(t, conexoesComDuplicata())

	for _, want := range []string{
		"Suspeitas de duplicata: mesmo tipo e mesma assinatura no nome (2)",
		"dockerregistry, assinatura " + assinaturaA,
		"DeviosACR - MPN - FULANO - SB",
		// Suspeita, não veredito: a saída precisa dizer que pode ser de propósito.
		"Pode ser intencional",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q:\n%s", want, out)
		}
	}
}

func TestWriteConnectionsSemDuplicataOmiteSecao(t *testing.T) {
	out := renderConexoes(t, conexoesDeExemplo())

	if strings.Contains(out, "Suspeitas de duplicata") {
		t.Errorf("sem duplicata não deveria ter a seção:\n%s", out)
	}
}
