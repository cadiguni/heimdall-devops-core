package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// devOpsResourceID é o application ID do Azure DevOps no Entra ID. O token
// precisa ser pedido para este GUID, não para uma URI de recurso — a própria
// documentação da Microsoft insiste nesse ponto.
//
// Confirmado em learn.microsoft.com/azure/devops/cli/entra-tokens.
const devOpsResourceID = "499b84ac-1321-427f-aa17-267ca6975798"

// devOpsAPIVersion é a versão da API REST usada nas chamadas.
const devOpsAPIVersion = "7.1"

// VariableValue é uma variável dentro de um Variable Group.
//
// Value é ponteiro de propósito: a API devolve null no valor das variáveis
// secretas, e "null" precisa ser distinguível de "string vazia".
type VariableValue struct {
	Value      *string `json:"value"`
	IsSecret   bool    `json:"isSecret"`
	IsReadOnly bool    `json:"isReadOnly"`
}

// VariableGroup é um Variable Group do Azure DevOps.
type VariableGroup struct {
	ID          int                      `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	Type        string                   `json:"type"`
	IsShared    bool                     `json:"isShared"`
	Variables   map[string]VariableValue `json:"variables"`
	ModifiedOn  time.Time                `json:"modifiedOn"`
	ModifiedBy  IdentityRef              `json:"modifiedBy"`
}

// IdentityRef é a referência a uma identidade do Azure DevOps. Exportada
// porque aparece em VariableGroup, e quem monta um grupo em teste precisa
// conseguir preenchê-la.
type IdentityRef struct {
	DisplayName string `json:"displayName"`
}

// ModifiedByName é quem mexeu por último, ou string vazia.
func (g VariableGroup) ModifiedByName() string {
	return g.ModifiedBy.DisplayName
}

// VariableGroupLister lista os Variable Groups de um projeto.
//
// Como no restante do módulo, a interface existe para o domínio ser exercitado
// sem chamar o serviço de verdade.
type VariableGroupLister interface {
	ListVariableGroups(ctx context.Context) ([]VariableGroup, error)
}

// DevOpsClient fala com a API REST do Azure DevOps de um projeto.
type DevOpsClient struct {
	organization string
	project      string
	baseURL      string

	httpClient *http.Client

	// token devolve o bearer a cada chamada. É função para o teste poder
	// substituir sem precisar de Entra ID.
	token func(ctx context.Context) (string, error)
}

// ProjectURL é o endereço do projeto, para o comando dizer contra o que está
// operando antes de mostrar qualquer coisa.
func (c *DevOpsClient) ProjectURL() string {
	return fmt.Sprintf("%s/%s/%s", c.baseURL, c.organization, c.project)
}

// NewDevOpsClient conecta usando DefaultAzureCredential, a mesma identidade dos
// demais comandos: localmente a sessão do 'az login'.
//
// Não existe caminho de PAT aqui de propósito. Um PAT é um segredo de longa
// duração que vazaria em histórico de shell e em log de pipeline, exatamente o
// que a ausência de access key evita no acesso ao Storage.
func NewDevOpsClient(organization, project string) (*DevOpsClient, error) {
	if organization == "" {
		return nil, fmt.Errorf("organização do Azure DevOps não informada")
	}
	if project == "" {
		return nil, fmt.Errorf("projeto do Azure DevOps não informado")
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("não foi possível obter credencial do Entra ID (tente 'az login'): %w", err)
	}

	return &DevOpsClient{
		organization: organization,
		project:      project,
		baseURL:      "https://dev.azure.com",
		httpClient:   http.DefaultClient,
		token:        tokenFromCredential(cred),
	}, nil
}

func tokenFromCredential(cred azcore.TokenCredential) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		tk, err := cred.GetToken(ctx, policy.TokenRequestOptions{
			Scopes: []string{devOpsResourceID + "/.default"},
		})
		if err != nil {
			return "", fmt.Errorf("não foi possível obter token para o Azure DevOps: %w", err)
		}
		return tk.Token, nil
	}
}

// ListVariableGroups lista os Variable Groups do projeto.
func (c *DevOpsClient) ListVariableGroups(ctx context.Context) ([]VariableGroup, error) {
	endpoint := fmt.Sprintf("%s/%s/%s/_apis/distributedtask/variablegroups?api-version=%s",
		c.baseURL, url.PathEscape(c.organization), url.PathEscape(c.project), devOpsAPIVersion)

	var payload struct {
		Count int             `json:"count"`
		Value []VariableGroup `json:"value"`
	}
	if err := c.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}

	return payload.Value, nil
}

func (c *DevOpsClient) getJSON(ctx context.Context, endpoint string, out interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("montando requisição: %w", err)
	}

	token, err := c.token(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("chamando %s: %w", c.ProjectURL(), err)
	}
	defer resp.Body.Close()

	if err := checkDevOpsStatus(resp, c.ProjectURL()); err != nil {
		return err
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("resposta inesperada de %s: %w", c.ProjectURL(), err)
	}
	return nil
}

// checkDevOpsStatus traduz os status que têm causa conhecida.
//
// O 203 é o caso traiçoeiro: quando o token não tem o escopo certo, o Azure
// DevOps devolve a página de login com status 203 em vez de 401, e o corpo
// decodifica em silêncio para uma lista vazia. Sem esta checagem, "token sem
// escopo" apareceria como "nenhum Variable Group".
func checkDevOpsStatus(resp *http.Response, projectURL string) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNonAuthoritativeInfo:
		return fmt.Errorf("o Azure DevOps devolveu a página de login para %s: "+
			"o token não é aceito por esta organização.\n"+
			"Confira se a assinatura do 'az login' está no tenant ligado à organização", projectURL)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("sem permissão em %s (HTTP %d): "+
			"sua conta precisa de acesso de leitura aos Variable Groups do projeto",
			projectURL, resp.StatusCode)
	case http.StatusNotFound:
		return fmt.Errorf("organização ou projeto não encontrado: %s", projectURL)
	default:
		return fmt.Errorf("resposta inesperada de %s: HTTP %d", projectURL, resp.StatusCode)
	}
}
