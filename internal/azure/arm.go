package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
)

// armAPIVersion é a versão usada na listagem de resource groups.
const armAPIVersion = "2021-04-01"

// armScope é o escopo do token para o plano de controle do Azure.
const armScope = "https://management.azure.com/.default"

// ResourceGroup é um resource group como o ARM o devolve, reduzido.
//
// As tags entram porque são o que diz de quem é o recurso quando não há state
// por trás — e é justamente o caso que interessa descobrir.
type ResourceGroup struct {
	Name     string            `json:"name"`
	Location string            `json:"location"`
	Tags     map[string]string `json:"tags"`
}

// ResourceGroupLister lista os resource groups de uma assinatura.
type ResourceGroupLister interface {
	ListResourceGroups(ctx context.Context) ([]ResourceGroup, error)
}

// ARMClient fala com o plano de controle do Azure de uma assinatura.
//
// É REST cru, como o DevOpsClient, em vez do SDK de recursos: a chamada é uma
// só, e uma dependência a mais para uma listagem não se paga.
type ARMClient struct {
	subscription string
	baseURL      string

	httpClient *http.Client
	token      func(ctx context.Context) (string, error)
}

// SubscriptionID é a assinatura contra a qual este cliente opera, para o
// comando poder dizer no cabeçalho contra o que está operando.
func (c *ARMClient) SubscriptionID() string { return c.subscription }

// NewARMClient conecta com DefaultAzureCredential, a mesma identidade dos
// demais comandos.
func NewARMClient(subscription string) (*ARMClient, error) {
	if subscription == "" {
		return nil, fmt.Errorf("assinatura não informada")
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("não foi possível obter credencial do Entra ID (tente 'az login'): %w", err)
	}

	return &ARMClient{
		subscription: subscription,
		baseURL:      "https://management.azure.com",
		httpClient:   http.DefaultClient,
		token:        armTokenFromCredential(cred),
	}, nil
}

func armTokenFromCredential(cred azcore.TokenCredential) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		tk, err := cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{armScope}})
		if err != nil {
			return "", fmt.Errorf("não foi possível obter token para o Azure Resource Manager: %w", err)
		}
		return tk.Token, nil
	}
}

// ListResourceGroups lista os resource groups da assinatura, seguindo a
// paginação até o fim.
//
// Seguir o nextLink não é detalhe: parar na primeira página devolveria uma
// lista incompleta que parece completa, e quem compara essa lista com os states
// concluiria que recursos não existem.
func (c *ARMClient) ListResourceGroups(ctx context.Context) ([]ResourceGroup, error) {
	endpoint := fmt.Sprintf("%s/subscriptions/%s/resourcegroups?api-version=%s",
		c.baseURL, url.PathEscape(c.subscription), armAPIVersion)

	var todos []ResourceGroup

	for endpoint != "" {
		var payload struct {
			Value    []ResourceGroup `json:"value"`
			NextLink string          `json:"nextLink"`
		}
		if err := c.getJSON(ctx, endpoint, &payload); err != nil {
			return nil, err
		}

		todos = append(todos, payload.Value...)
		endpoint = payload.NextLink
	}

	return todos, nil
}

func (c *ARMClient) getJSON(ctx context.Context, endpoint string, out interface{}) error {
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
		return fmt.Errorf("chamando o Azure Resource Manager: %w", err)
	}
	defer resp.Body.Close()

	if err := checkARMStatus(resp, c.subscription); err != nil {
		return err
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("resposta inesperada do Azure Resource Manager: %w", err)
	}
	return nil
}

// checkARMStatus separa os status cuja causa muda o que a pessoa deve fazer.
//
// O 403 é o que importa distinguir: ter acesso de dados ao Storage não dá
// acesso ao plano de controle, e nesse caso a listagem volta vazia ou negada —
// o que não pode ser lido como "a assinatura não tem resource group".
func checkARMStatus(resp *http.Response, subscription string) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("sem permissão de leitura na assinatura %s (HTTP %d): "+
			"acesso de dados ao Storage não inclui o plano de controle — é preciso "+
			"papel de leitura na assinatura", subscription, resp.StatusCode)
	case http.StatusNotFound:
		return fmt.Errorf("assinatura %s não encontrada, ou invisível para esta conta", subscription)
	default:
		corpo := resumoDoCorpo(resp)
		return fmt.Errorf("resposta inesperada do Azure Resource Manager para %s: HTTP %d%s",
			subscription, resp.StatusCode, corpo)
	}
}

// resumoDoCorpo traz o começo do corpo do erro, que costuma ter o código real.
func resumoDoCorpo(resp *http.Response) string {
	buf := make([]byte, 400)
	n, _ := resp.Body.Read(buf)
	if n == 0 {
		return ""
	}
	return ": " + strings.TrimSpace(string(buf[:n]))
}
