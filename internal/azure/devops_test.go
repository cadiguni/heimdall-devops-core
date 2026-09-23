package azure

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// respostaDeGrupos é uma resposta como a documentada pela API: a variável
// secreta vem com value null e isSecret true.
const respostaDeGrupos = `{
  "count": 2,
  "value": [
    {
      "id": 2,
      "name": "terraform-dev",
      "type": "Vsts",
      "description": "backend e credenciais de dev",
      "variables": {
        "AMBIENTE": {"value": "dev"},
        "ARM_CLIENT_SECRET": {"value": null, "isSecret": true}
      },
      "modifiedOn": "2026-09-10T11:00:00Z",
      "modifiedBy": {"displayName": "Fulano de Tal"}
    },
    {
      "id": 7,
      "name": "cofre-prod",
      "type": "AzureKeyVault",
      "isShared": true,
      "variables": {"senha-banco": {"value": null, "isSecret": true}},
      "modifiedOn": "2026-08-01T09:30:00Z",
      "modifiedBy": {"displayName": "Beltrana"}
    }
  ]
}`

// clienteDevOpsFake monta um DevOpsClient que fala com o transporte fake e usa
// um token fixo, sem passar pelo Entra ID.
func clienteDevOpsFake(t *testing.T, transport *fakeTransport) *DevOpsClient {
	t.Helper()

	return &DevOpsClient{
		organization: "minhaorg",
		project:      "MeuProjeto",
		baseURL:      "https://dev.azure.com",
		httpClient:   &http.Client{Transport: transportAdapter{transport}},
		token: func(context.Context) (string, error) {
			return "token-de-teste", nil
		},
	}
}

// transportAdapter usa o mesmo fakeTransport dos testes de blob, que já
// implementa Do(*http.Request).
type transportAdapter struct{ f *fakeTransport }

func (a transportAdapter) RoundTrip(req *http.Request) (*http.Response, error) {
	return a.f.Do(req)
}

func TestListVariableGroups(t *testing.T) {
	transport := &fakeTransport{bodies: []string{respostaDeGrupos}}

	groups, err := clienteDevOpsFake(t, transport).ListVariableGroups(context.Background())
	if err != nil {
		t.Fatalf("ListVariableGroups: %v", err)
	}

	if len(groups) != 2 {
		t.Fatalf("grupos = %d, quero 2", len(groups))
	}

	dev := groups[0]
	if dev.Name != "terraform-dev" || dev.ID != 2 {
		t.Errorf("primeiro grupo = %+v", dev)
	}
	if dev.ModifiedByName() != "Fulano de Tal" {
		t.Errorf("ModifiedByName = %q", dev.ModifiedByName())
	}

	// O valor de variável secreta vem null: precisa ser distinguível de vazio.
	secreta := dev.Variables["ARM_CLIENT_SECRET"]
	if !secreta.IsSecret {
		t.Error("ARM_CLIENT_SECRET deveria estar marcada como secreta")
	}
	if secreta.Value != nil {
		t.Errorf("valor de variável secreta = %v, quero nil", *secreta.Value)
	}

	comum := dev.Variables["AMBIENTE"]
	if comum.IsSecret || comum.Value == nil || *comum.Value != "dev" {
		t.Errorf("AMBIENTE = %+v", comum)
	}
}

func TestListVariableGroupsMontaAURL(t *testing.T) {
	transport := &fakeTransport{bodies: []string{`{"count":0,"value":[]}`}}

	if _, err := clienteDevOpsFake(t, transport).ListVariableGroups(context.Background()); err != nil {
		t.Fatalf("ListVariableGroups: %v", err)
	}

	req := transport.requests[0]
	if req.URL.Path != "/minhaorg/MeuProjeto/_apis/distributedtask/variablegroups" {
		t.Errorf("caminho = %q", req.URL.Path)
	}
	if got := req.URL.Query().Get("api-version"); got != devOpsAPIVersion {
		t.Errorf("api-version = %q, quero %q", got, devOpsAPIVersion)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer token-de-teste" {
		t.Errorf("Authorization = %q", got)
	}
}

// O 203 é o caso que enganaria: o Azure DevOps devolve a página de login em vez
// de 401, e o corpo decodificaria em silêncio para lista vazia.
func TestListVariableGroups203ViraErro(t *testing.T) {
	transport := &fakeTransport{
		status: http.StatusNonAuthoritativeInfo,
		bodies: []string{"<html>sign in</html>"},
	}

	_, err := clienteDevOpsFake(t, transport).ListVariableGroups(context.Background())

	if err == nil {
		t.Fatal("erro = nil, quero falha no 203")
	}
	if !strings.Contains(err.Error(), "página de login") {
		t.Errorf("erro não explica o 203: %v", err)
	}
	if !strings.Contains(err.Error(), "tenant") {
		t.Errorf("erro não diz o que conferir: %v", err)
	}
}

func TestListVariableGroupsStatusConhecidos(t *testing.T) {
	casos := map[int]string{
		http.StatusUnauthorized: "sem permissão",
		http.StatusForbidden:    "sem permissão",
		http.StatusNotFound:     "não encontrado",
		http.StatusBadGateway:   "resposta inesperada",
	}

	for status, esperado := range casos {
		t.Run(http.StatusText(status), func(t *testing.T) {
			transport := &fakeTransport{status: status, bodies: []string{"{}"}}

			_, err := clienteDevOpsFake(t, transport).ListVariableGroups(context.Background())
			if err == nil {
				t.Fatal("erro = nil, quero falha")
			}
			if !strings.Contains(err.Error(), esperado) {
				t.Errorf("erro = %v, quero conter %q", err, esperado)
			}
			// Sempre dizer contra qual projeto a chamada falhou.
			if !strings.Contains(err.Error(), "MeuProjeto") {
				t.Errorf("erro não identifica o projeto: %v", err)
			}
		})
	}
}

func TestListVariableGroupsCorpoInvalido(t *testing.T) {
	transport := &fakeTransport{bodies: []string{"isso não é json"}}

	if _, err := clienteDevOpsFake(t, transport).ListVariableGroups(context.Background()); err == nil {
		t.Error("erro = nil, quero falha ao decodificar")
	}
}

func TestListVariableGroupsPropagaErroDeToken(t *testing.T) {
	client := clienteDevOpsFake(t, &fakeTransport{bodies: []string{"{}"}})
	client.token = func(context.Context) (string, error) {
		return "", io.ErrUnexpectedEOF
	}

	if _, err := client.ListVariableGroups(context.Background()); err == nil {
		t.Error("erro = nil, quero a falha de token")
	}
}

// A validação acontece antes de tentar credencial: errar o parâmetro não deve
// custar uma ida ao Entra ID.
func TestNewDevOpsClientValidaParametros(t *testing.T) {
	casos := map[string][2]string{
		"sem organização": {"", "MeuProjeto"},
		"sem projeto":     {"minhaorg", ""},
	}

	for name, args := range casos {
		t.Run(name, func(t *testing.T) {
			client, err := NewDevOpsClient(args[0], args[1])
			if err == nil {
				t.Fatalf("erro = nil, quero falha (client=%v)", client)
			}
			if client != nil {
				t.Errorf("client = %v, quero nil", client)
			}
		})
	}
}

func TestProjectURL(t *testing.T) {
	client := clienteDevOpsFake(t, &fakeTransport{})

	if got := client.ProjectURL(); got != "https://dev.azure.com/minhaorg/MeuProjeto" {
		t.Errorf("ProjectURL = %q", got)
	}
}

// O escopo do token precisa ser o application ID do Azure DevOps. Token pedido
// para outro recurso volta com 203, que é justamente o erro difícil de
// diagnosticar.
func TestEscopoDoTokenEhOApplicationIDDoDevOps(t *testing.T) {
	const documentado = "499b84ac-1321-427f-aa17-267ca6975798"

	if devOpsResourceID != documentado {
		t.Errorf("devOpsResourceID = %q, quero %q", devOpsResourceID, documentado)
	}
}

const respostaDeConexoes = `{
  "count": 2,
  "value": [
    {
      "id": "5e47a0d8-c745-44f8-8f93-784f18ff31c4",
      "name": "arm-federada",
      "type": "azurerm",
      "url": "https://management.azure.com/",
      "isReady": true,
      "isShared": false,
      "owner": "Library",
      "createdBy": {"displayName": "Fulano de Tal"},
      "authorization": {
        "scheme": "WorkloadIdentityFederation",
        "parameters": {"serviceprincipalid": "abc", "tenantid": "def"}
      }
    },
    {
      "id": "7f22b1c9-0000-4444-9999-111111111111",
      "name": "arm-com-secret",
      "type": "azurerm",
      "isReady": false,
      "authorization": {"scheme": "ServicePrincipal", "parameters": {"serviceprincipalid": "xyz"}}
    }
  ]
}`

func TestListServiceEndpoints(t *testing.T) {
	transport := &fakeTransport{bodies: []string{respostaDeConexoes}}

	endpoints, err := clienteDevOpsFake(t, transport).ListServiceEndpoints(context.Background())
	if err != nil {
		t.Fatalf("ListServiceEndpoints: %v", err)
	}

	if len(endpoints) != 2 {
		t.Fatalf("conexões = %d, quero 2", len(endpoints))
	}

	federada := endpoints[0]
	if federada.Name != "arm-federada" || federada.Type != "azurerm" {
		t.Errorf("primeira = %+v", federada)
	}
	if federada.Authorization.Scheme != "WorkloadIdentityFederation" {
		t.Errorf("scheme = %q", federada.Authorization.Scheme)
	}
	if !federada.IsReady {
		t.Error("IsReady deveria ser true")
	}
	if federada.CreatedBy.DisplayName != "Fulano de Tal" {
		t.Errorf("CreatedBy = %q", federada.CreatedBy.DisplayName)
	}

	if endpoints[1].IsReady {
		t.Error("a segunda conexão deveria vir como não pronta")
	}
}

// EndpointAuthorization não declara "parameters" de propósito: o campo carrega
// material de credencial, e o que não é preciso não deve nem ser decodificado.
func TestParametrosDeAutorizacaoNaoSaoDecodificados(t *testing.T) {
	transport := &fakeTransport{bodies: []string{respostaDeConexoes}}

	endpoints, err := clienteDevOpsFake(t, transport).ListServiceEndpoints(context.Background())
	if err != nil {
		t.Fatalf("ListServiceEndpoints: %v", err)
	}

	asJSON, err := json.Marshal(endpoints)
	if err != nil {
		t.Fatalf("serializando: %v", err)
	}
	for _, parametro := range []string{"serviceprincipalid", "tenantid", "abc", "xyz"} {
		if strings.Contains(string(asJSON), parametro) {
			t.Errorf("parâmetro de autorização %q entrou na struct: %s", parametro, asJSON)
		}
	}
}

func TestListServiceEndpointsMontaAURL(t *testing.T) {
	transport := &fakeTransport{bodies: []string{`{"count":0,"value":[]}`}}

	if _, err := clienteDevOpsFake(t, transport).ListServiceEndpoints(context.Background()); err != nil {
		t.Fatalf("ListServiceEndpoints: %v", err)
	}

	req := transport.requests[0]
	if req.URL.Path != "/minhaorg/MeuProjeto/_apis/serviceendpoint/endpoints" {
		t.Errorf("caminho = %q", req.URL.Path)
	}
	if got := req.URL.Query().Get("api-version"); got != devOpsAPIVersion {
		t.Errorf("api-version = %q", got)
	}
}

func TestListServiceEndpointsPropagaStatus(t *testing.T) {
	transport := &fakeTransport{status: http.StatusForbidden, bodies: []string{"{}"}}

	_, err := clienteDevOpsFake(t, transport).ListServiceEndpoints(context.Background())
	if err == nil {
		t.Fatal("erro = nil, quero falha no 403")
	}
	if !strings.Contains(err.Error(), "sem permissão") {
		t.Errorf("erro = %v", err)
	}
}
