package azure

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func clienteARMFake(t *testing.T, transport *fakeTransport) *ARMClient {
	t.Helper()

	return &ARMClient{
		subscription: "92dbecc2-c36d-4af2-887d-3681969e5850",
		baseURL:      "https://management.azure.com",
		httpClient:   &http.Client{Transport: transportAdapter{transport}},
		token: func(context.Context) (string, error) {
			return "token-de-teste", nil
		},
	}
}

func TestListResourceGroups(t *testing.T) {
	const resposta = `{"value":[
      {"name":"atena-dev-rg","location":"eastus","tags":{"Setor":"NAP","Responsável":"a@b.com"}},
      {"name":"rg-gvCert","location":"eastus"}
    ]}`

	transport := &fakeTransport{bodies: []string{resposta}}

	grupos, err := clienteARMFake(t, transport).ListResourceGroups(context.Background())
	if err != nil {
		t.Fatalf("ListResourceGroups: %v", err)
	}
	if len(grupos) != 2 {
		t.Fatalf("grupos = %+v", grupos)
	}
	if grupos[0].Name != "atena-dev-rg" || grupos[0].Location != "eastus" {
		t.Errorf("grupo = %+v", grupos[0])
	}
	if grupos[0].Tags["Setor"] != "NAP" {
		t.Errorf("tags = %v", grupos[0].Tags)
	}
	if len(grupos[1].Tags) != 0 {
		t.Errorf("grupo sem tag veio com tags: %v", grupos[1].Tags)
	}
}

// Parar na primeira página devolveria uma lista incompleta que parece
// completa, e quem compara essa lista com os states concluiria que recursos
// não existem.
func TestListResourceGroupsSegueAPaginacao(t *testing.T) {
	transport := &fakeTransport{bodies: []string{
		`{"value":[{"name":"primeira-rg"}],"nextLink":"https://management.azure.com/proxima"}`,
		`{"value":[{"name":"segunda-rg"}]}`,
	}}

	grupos, err := clienteARMFake(t, transport).ListResourceGroups(context.Background())
	if err != nil {
		t.Fatalf("ListResourceGroups: %v", err)
	}
	if len(grupos) != 2 {
		t.Fatalf("grupos = %+v", grupos)
	}
	if len(transport.requests) != 2 {
		t.Errorf("requisições = %d, queria 2", len(transport.requests))
	}
	if transport.requests[1].URL.Path != "/proxima" {
		t.Errorf("não seguiu o nextLink: %s", transport.requests[1].URL)
	}
}

// Acesso de dados ao Storage não inclui plano de controle. Sem distinguir
// isso, "sem permissão" apareceria como "a assinatura não tem resource group".
func TestListResourceGroupsSemPermissaoDizOQueFalta(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		transport := &fakeTransport{status: status, bodies: []string{"{}"}}

		_, err := clienteARMFake(t, transport).ListResourceGroups(context.Background())
		if err == nil {
			t.Fatalf("HTTP %d não virou erro", status)
		}
		if !strings.Contains(err.Error(), "plano de controle") {
			t.Errorf("HTTP %d: erro não explica a permissão: %v", status, err)
		}
	}
}

func TestListResourceGroupsAssinaturaInexistente(t *testing.T) {
	transport := &fakeTransport{status: http.StatusNotFound, bodies: []string{"{}"}}

	_, err := clienteARMFake(t, transport).ListResourceGroups(context.Background())
	if err == nil || !strings.Contains(err.Error(), "não encontrada") {
		t.Errorf("erro = %v", err)
	}
}

func TestListResourceGroupsMandaOBearer(t *testing.T) {
	transport := &fakeTransport{bodies: []string{`{"value":[]}`}}

	if _, err := clienteARMFake(t, transport).ListResourceGroups(context.Background()); err != nil {
		t.Fatalf("ListResourceGroups: %v", err)
	}
	if got := transport.requests[0].Header.Get("Authorization"); got != "Bearer token-de-teste" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestNewARMClientExigeAssinatura(t *testing.T) {
	if _, err := NewARMClient(""); err == nil {
		t.Error("assinatura vazia passou")
	}
}
