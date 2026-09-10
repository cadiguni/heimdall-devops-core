package azure

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
)

// DefaultEndpointSuffix é o sufixo do Azure público. Clouds soberanas
// (Government, China) usam outro, daí ser configurável.
const DefaultEndpointSuffix = "blob.core.windows.net"

// Blob é o subconjunto de propriedades de um blob que o heimdall usa.
type Blob struct {
	Name         string
	LastModified time.Time
	SizeBytes    int64

	// LeaseState é o estado do lease: available, leased, expired, breaking ou
	// broken. É por aqui que se enxerga o lock do backend azurerm.
	LeaseState string

	// Metadata vem com as chaves normalizadas em minúsculas: o Azure trata
	// nomes de metadata como case-insensitive e não garante a capitalização de
	// volta, então comparar chave crua é frágil.
	Metadata map[string]string
}

// BlobLister lista blobs de um container.
//
// A interface existe para a lógica de domínio poder ser exercitada sem Azure —
// o adapter real é o único ponto que fala com a nuvem.
type BlobLister interface {
	ListBlobs(ctx context.Context, prefix string) ([]Blob, error)
}

// ContainerClient lê um container de Blob Storage autenticando com a
// identidade do ambiente.
type ContainerClient struct {
	client *container.Client
	url    string
}

// ContainerURL monta a URL de um container.
func ContainerURL(account, containerName, endpointSuffix string) (string, error) {
	if account == "" {
		return "", fmt.Errorf("nome da storage account não informado")
	}
	if containerName == "" {
		return "", fmt.Errorf("nome do container não informado")
	}
	if endpointSuffix == "" {
		endpointSuffix = DefaultEndpointSuffix
	}
	return fmt.Sprintf("https://%s.%s/%s", account, endpointSuffix, containerName), nil
}

// NewContainerClient conecta usando DefaultAzureCredential, que resolve, nesta
// ordem, variáveis de ambiente, identidade gerenciada e a sessão do 'az login'.
//
// Não existe caminho de access key aqui de propósito: chave em flag vaza no
// histórico do shell, em 'ps' e no log da pipeline.
func NewContainerClient(account, containerName, endpointSuffix string) (*ContainerClient, error) {
	url, err := ContainerURL(account, containerName, endpointSuffix)
	if err != nil {
		return nil, err
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("não foi possível obter credencial do Azure (tente 'az login'): %w", err)
	}

	client, err := container.NewClient(url, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("não foi possível abrir o container %s: %w", url, err)
	}

	return &ContainerClient{client: client, url: url}, nil
}

// URL é o endereço do container, para o comando poder mostrar contra o que está
// operando antes de fazer qualquer coisa.
func (c *ContainerClient) URL() string {
	return c.url
}

// ListBlobs lista os blobs sob o prefixo, já com metadata.
func (c *ContainerClient) ListBlobs(ctx context.Context, prefix string) ([]Blob, error) {
	opts := &container.ListBlobsFlatOptions{
		// Metadata precisa ser pedido explicitamente; sem isso o lock do
		// backend azurerm não vem na listagem.
		Include: container.ListBlobsInclude{Metadata: true},
	}
	if prefix != "" {
		opts.Prefix = &prefix
	}

	var blobs []Blob
	pager := c.client.NewListBlobsFlatPager(opts)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listando blobs de %s: %w", c.url, err)
		}
		if page.Segment == nil {
			continue
		}
		for _, item := range page.Segment.BlobItems {
			if item == nil || item.Name == nil {
				continue
			}
			blobs = append(blobs, newBlob(item))
		}
	}

	return blobs, nil
}

func newBlob(item *container.BlobItem) Blob {
	blob := Blob{
		Name:     *item.Name,
		Metadata: normalizeMetadata(item.Metadata),
	}

	if props := item.Properties; props != nil {
		if props.LastModified != nil {
			blob.LastModified = *props.LastModified
		}
		if props.ContentLength != nil {
			blob.SizeBytes = *props.ContentLength
		}
		if props.LeaseState != nil {
			blob.LeaseState = string(*props.LeaseState)
		}
	}

	return blob
}

// normalizeMetadata baixa as chaves para minúsculas e descarta valores nulos.
func normalizeMetadata(raw map[string]*string) map[string]string {
	if len(raw) == 0 {
		return nil
	}

	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if v == nil {
			continue
		}
		out[strings.ToLower(k)] = *v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
