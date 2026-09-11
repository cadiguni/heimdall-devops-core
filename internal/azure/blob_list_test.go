package azure

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
)

// fakeTransport responde às chamadas do SDK com XML fixo e guarda as
// requisições, para os testes poderem afirmar o que foi pedido ao serviço.
type fakeTransport struct {
	bodies   []string
	status   int
	requests []*http.Request
}

func (f *fakeTransport) Do(req *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, req)

	status := f.status
	if status == 0 {
		status = http.StatusOK
	}

	body := ""
	if len(f.bodies) > 0 {
		body = f.bodies[min(len(f.requests)-1, len(f.bodies)-1)]
	}

	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/xml"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// clientComFake monta um ContainerClient que fala com o transporte fake em vez
// do Azure. Sem credencial: a autenticação não faz parte do que está sob teste.
func clientComFake(t *testing.T, transport *fakeTransport) *ContainerClient {
	t.Helper()

	const url = "https://stfake.blob.core.windows.net/nap"

	client, err := container.NewClientWithNoCredential(url, &container.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Transport: transport,
			// Sem retry: um teste de erro não precisa esperar backoff.
			Retry: policy.RetryOptions{MaxRetries: -1},
		},
	})
	if err != nil {
		t.Fatalf("montando client: %v", err)
	}

	return &ContainerClient{client: client, url: url}
}

func listaVazia(nextMarker string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<EnumerationResults ServiceEndpoint="https://stfake.blob.core.windows.net/" ContainerName="nap">
  <Blobs></Blobs>
  <NextMarker>` + nextMarker + `</NextMarker>
</EnumerationResults>`
}

func listaCom(blobsXML string, nextMarker string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<EnumerationResults ServiceEndpoint="https://stfake.blob.core.windows.net/" ContainerName="nap">
  <Blobs>` + blobsXML + `</Blobs>
  <NextMarker>` + nextMarker + `</NextMarker>
</EnumerationResults>`
}

const blobTravado = `
    <Blob>
      <Name>prod/app.tfstate</Name>
      <Properties>
        <Last-Modified>Thu, 10 Sep 2026 11:00:00 GMT</Last-Modified>
        <Content-Length>85565</Content-Length>
        <LeaseState>leased</LeaseState>
        <LeaseStatus>locked</LeaseStatus>
      </Properties>
      <Metadata>
        <Terraformlockid>YWJj</Terraformlockid>
      </Metadata>
    </Blob>`

// Sem include=metadata o serviço não devolve a metadata, e a detecção de lock
// do backend azurerm morre em silêncio. É o detalhe mais fácil de perder.
func TestListBlobsPedeMetadataAoServico(t *testing.T) {
	transport := &fakeTransport{bodies: []string{listaVazia("")}}

	if _, err := clientComFake(t, transport).ListBlobs(context.Background(), ""); err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}

	if len(transport.requests) != 1 {
		t.Fatalf("requisições = %d, quero 1", len(transport.requests))
	}

	query := transport.requests[0].URL.Query()
	if got := query.Get("include"); !strings.Contains(got, "metadata") {
		t.Errorf("include = %q, quero conter metadata", got)
	}
	if got := query.Get("comp"); got != "list" {
		t.Errorf("comp = %q, quero list", got)
	}
}

func TestListBlobsRepassaPrefixoAoServico(t *testing.T) {
	transport := &fakeTransport{bodies: []string{listaVazia("")}}

	if _, err := clientComFake(t, transport).ListBlobs(context.Background(), "prod/"); err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}

	if got := transport.requests[0].URL.Query().Get("prefix"); got != "prod/" {
		t.Errorf("prefix = %q, quero prod/", got)
	}
}

// Sem prefixo o parâmetro não pode ser mandado vazio.
func TestListBlobsSemPrefixoNaoMandaParametro(t *testing.T) {
	transport := &fakeTransport{bodies: []string{listaVazia("")}}

	if _, err := clientComFake(t, transport).ListBlobs(context.Background(), ""); err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}

	if _, presente := transport.requests[0].URL.Query()["prefix"]; presente {
		t.Errorf("prefix foi enviado sem necessidade: %s", transport.requests[0].URL.RawQuery)
	}
}

// Um container com muitos states devolve a listagem paginada; parar na primeira
// página esconderia states.
func TestListBlobsPercorreTodasAsPaginas(t *testing.T) {
	pagina1 := listaCom(`
    <Blob>
      <Name>dev/a.tfstate</Name>
      <Properties><Content-Length>10</Content-Length><LeaseState>available</LeaseState></Properties>
    </Blob>`, "marcador-da-proxima")
	pagina2 := listaCom(`
    <Blob>
      <Name>dev/b.tfstate</Name>
      <Properties><Content-Length>20</Content-Length><LeaseState>available</LeaseState></Properties>
    </Blob>`, "")

	transport := &fakeTransport{bodies: []string{pagina1, pagina2}}

	blobs, err := clientComFake(t, transport).ListBlobs(context.Background(), "")
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}

	if len(blobs) != 2 {
		t.Fatalf("blobs = %d, quero 2: %+v", len(blobs), blobs)
	}
	if blobs[0].Name != "dev/a.tfstate" || blobs[1].Name != "dev/b.tfstate" {
		t.Errorf("blobs fora de ordem ou errados: %+v", blobs)
	}

	if len(transport.requests) != 2 {
		t.Fatalf("requisições = %d, quero 2", len(transport.requests))
	}
	if got := transport.requests[1].URL.Query().Get("marker"); got != "marcador-da-proxima" {
		t.Errorf("marker da segunda página = %q", got)
	}
}

func TestListBlobsConverteCampos(t *testing.T) {
	transport := &fakeTransport{bodies: []string{listaCom(blobTravado, "")}}

	blobs, err := clientComFake(t, transport).ListBlobs(context.Background(), "")
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if len(blobs) != 1 {
		t.Fatalf("blobs = %d, quero 1", len(blobs))
	}

	blob := blobs[0]
	if blob.Name != "prod/app.tfstate" {
		t.Errorf("Name = %q", blob.Name)
	}
	if blob.SizeBytes != 85565 {
		t.Errorf("SizeBytes = %d", blob.SizeBytes)
	}
	if blob.LeaseState != "leased" {
		t.Errorf("LeaseState = %q", blob.LeaseState)
	}
	if !blob.LastModified.Equal(time.Date(2026, 9, 10, 11, 0, 0, 0, time.UTC)) {
		t.Errorf("LastModified = %v", blob.LastModified)
	}
	// O Azure devolve a chave capitalizada; o domínio procura em minúscula.
	if blob.Metadata["terraformlockid"] != "YWJj" {
		t.Errorf("metadata = %+v", blob.Metadata)
	}
}

func TestListBlobsIgnoraItemSemNome(t *testing.T) {
	semNome := `
    <Blob>
      <Properties><Content-Length>10</Content-Length></Properties>
    </Blob>`
	transport := &fakeTransport{bodies: []string{listaCom(semNome+blobTravado, "")}}

	blobs, err := clientComFake(t, transport).ListBlobs(context.Background(), "")
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}

	if len(blobs) != 1 || blobs[0].Name != "prod/app.tfstate" {
		t.Errorf("blobs = %+v, quero só o que tem nome", blobs)
	}
}

// Resposta sem o bloco <Blobs> não pode derrubar a listagem.
func TestListBlobsResponseSemSegmento(t *testing.T) {
	semBlobs := `<?xml version="1.0" encoding="utf-8"?>
<EnumerationResults ServiceEndpoint="https://stfake.blob.core.windows.net/" ContainerName="nap">
  <NextMarker></NextMarker>
</EnumerationResults>`
	transport := &fakeTransport{bodies: []string{semBlobs}}

	blobs, err := clientComFake(t, transport).ListBlobs(context.Background(), "")
	if err != nil {
		t.Fatalf("ListBlobs: %v", err)
	}
	if len(blobs) != 0 {
		t.Errorf("blobs = %+v, quero nenhum", blobs)
	}
}

func TestListBlobsPropagaErroDoServico(t *testing.T) {
	transport := &fakeTransport{
		status: http.StatusForbidden,
		bodies: []string{`<?xml version="1.0"?><Error><Code>AuthorizationPermissionMismatch</Code></Error>`},
	}

	_, err := clientComFake(t, transport).ListBlobs(context.Background(), "")

	if err == nil {
		t.Fatal("erro = nil, quero falha no 403")
	}
	// A mensagem precisa dizer contra qual container a leitura falhou: o 403 de
	// role faltando é o erro mais provável em uso real.
	if !strings.Contains(err.Error(), "stfake.blob.core.windows.net/nap") {
		t.Errorf("erro não identifica o container: %v", err)
	}
}

func TestDownloadBlob(t *testing.T) {
	const conteudo = `{"version":4,"serial":7}`
	transport := &fakeTransport{bodies: []string{conteudo}}

	body, err := clientComFake(t, transport).DownloadBlob(context.Background(), "prod/app.tfstate")
	if err != nil {
		t.Fatalf("DownloadBlob: %v", err)
	}
	defer body.Close()

	lido, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("lendo corpo: %v", err)
	}
	if string(lido) != conteudo {
		t.Errorf("conteúdo = %q, quero %q", lido, conteudo)
	}

	// O caminho do blob precisa ir na URL, não em query.
	if path := transport.requests[0].URL.Path; path != "/nap/prod/app.tfstate" {
		t.Errorf("path = %q", path)
	}
}

func TestDownloadBlobPropagaErro(t *testing.T) {
	transport := &fakeTransport{
		status: http.StatusNotFound,
		bodies: []string{`<?xml version="1.0"?><Error><Code>BlobNotFound</Code></Error>`},
	}

	_, err := clientComFake(t, transport).DownloadBlob(context.Background(), "nao/existe.tfstate")

	if err == nil {
		t.Fatal("erro = nil, quero falha no 404")
	}
	if !strings.Contains(err.Error(), "nao/existe.tfstate") {
		t.Errorf("erro não cita o blob pedido: %v", err)
	}
}

func TestListBlobsRespeitaContextoCancelado(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	transport := &fakeTransport{bodies: []string{listaVazia("")}}

	if _, err := clientComFake(t, transport).ListBlobs(ctx, ""); err == nil {
		t.Error("erro = nil, quero falha com contexto cancelado")
	}
}
