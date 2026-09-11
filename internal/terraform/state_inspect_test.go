package terraform

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func inspectFixture(t *testing.T, name string) *StateInspection {
	t.Helper()

	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("abrindo fixture: %v", err)
	}
	defer f.Close()

	s, err := InspectState(f, name)
	if err != nil {
		t.Fatalf("InspectState(%s): %v", name, err)
	}
	return s
}

// A fixture tem módulo, count e data source — os três casos que complicam a
// remontagem do endereço.
func TestInspectStateMontaEnderecos(t *testing.T) {
	s := inspectFixture(t, "state_com_recursos.tfstate")

	want := []string{
		"data.terraform_remote_state.vazio",
		"module.filho.terraform_data.interno",
		"terraform_data.contado[0]",
		"terraform_data.contado[1]",
		"terraform_data.um",
	}

	if len(s.Resources) != len(want) {
		t.Fatalf("recursos = %d, quero %d: %+v", len(s.Resources), len(want), s.Resources)
	}
	for i, address := range want {
		if s.Resources[i].Address != address {
			t.Errorf("posição %d = %q, quero %q", i, s.Resources[i].Address, address)
		}
	}
}

func TestInspectStateContaInstanciasNaoBlocos(t *testing.T) {
	s := inspectFixture(t, "state_com_recursos.tfstate")

	// terraform_data.um + contado[0] + contado[1] + module.filho.interno
	if s.Managed != 4 {
		t.Errorf("Managed = %d, quero 4", s.Managed)
	}
	if s.Data != 1 {
		t.Errorf("Data = %d, quero 1", s.Data)
	}
	if s.Empty() {
		t.Error("Empty() = true num state com recursos")
	}
}

func TestInspectStateMetadados(t *testing.T) {
	s := inspectFixture(t, "state_com_recursos.tfstate")

	if s.Version != 4 {
		t.Errorf("Version = %d", s.Version)
	}
	if s.TerraformVersion == "" {
		t.Error("TerraformVersion vazio")
	}
	if s.Serial == 0 {
		t.Error("Serial = 0")
	}
	if s.Lineage == "" {
		t.Error("Lineage vazio")
	}
	if len(s.Outputs) != 1 || s.Outputs[0] != "resultado" {
		t.Errorf("Outputs = %v, quero [resultado]", s.Outputs)
	}
	if p := s.Resources[0].Provider; p != "terraform.io/builtin/terraform" {
		t.Errorf("Provider = %q, esperava sem o embrulho provider[...]", p)
	}
}

// O state de 181 bytes é exatamente o que aparece no container real; precisa
// ser reconhecido como vazio, não como erro.
func TestInspectStateVazio(t *testing.T) {
	s := inspectFixture(t, "state_vazio.tfstate")

	if !s.Empty() {
		t.Errorf("Empty() = false: %+v", s)
	}
	if s.Managed != 0 || s.Data != 0 {
		t.Errorf("contagens = %d/%d, quero 0/0", s.Managed, s.Data)
	}
	// Precisa serializar como [] e não null.
	if s.Resources == nil {
		t.Error("Resources = nil, quero slice vazio")
	}
}

// Nenhum valor de atributo pode sair do state para a inspeção.
func TestInspectStateNaoCarregaAtributos(t *testing.T) {
	const secret = "SENHA-NO-STATE"

	state := `{
      "version": 4,
      "terraform_version": "1.14.5",
      "serial": 3,
      "lineage": "abc",
      "outputs": {"conn": {"value": "` + secret + `", "type": "string"}},
      "resources": [{
        "mode": "managed", "type": "azurerm_key_vault_secret", "name": "s",
        "provider": "provider[\"registry.terraform.io/hashicorp/azurerm\"]",
        "instances": [{"schema_version": 0, "attributes": {"value": "` + secret + `"}}]
      }]
    }`

	s, err := InspectState(strings.NewReader(state), "x.tfstate")
	if err != nil {
		t.Fatalf("InspectState: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteStateReport(&buf, s); err != nil {
		t.Fatalf("WriteStateReport: %v", err)
	}
	if strings.Contains(buf.String(), secret) {
		t.Errorf("relatório vazou valor de atributo:\n%s", buf.String())
	}

	// O nome do output aparece; o valor, não.
	if len(s.Outputs) != 1 || s.Outputs[0] != "conn" {
		t.Errorf("Outputs = %v", s.Outputs)
	}
	if !strings.Contains(buf.String(), "conn") {
		t.Errorf("o nome do output deveria aparecer:\n%s", buf.String())
	}
}

func TestInspectStateTainted(t *testing.T) {
	state := `{"version":4,"serial":1,"resources":[{
      "mode":"managed","type":"azurerm_storage_account","name":"s","provider":"p",
      "instances":[{"status":"tainted"}]}]}`

	s, err := InspectState(strings.NewReader(state), "x.tfstate")
	if err != nil {
		t.Fatalf("InspectState: %v", err)
	}
	if !s.Resources[0].Tainted {
		t.Error("recurso tainted não foi marcado")
	}

	var buf bytes.Buffer
	if err := WriteStateReport(&buf, s); err != nil {
		t.Fatalf("WriteStateReport: %v", err)
	}
	if !strings.Contains(buf.String(), "(tainted)") {
		t.Errorf("tainted não apareceu no relatório:\n%s", buf.String())
	}
}

func TestInspectStateForEachUsaChaveDeTexto(t *testing.T) {
	state := `{"version":4,"serial":1,"resources":[{
      "mode":"managed","type":"azurerm_subnet","name":"sub","provider":"p",
      "instances":[{"index_key":"app"},{"index_key":"db"}]}]}`

	s, err := InspectState(strings.NewReader(state), "x.tfstate")
	if err != nil {
		t.Fatalf("InspectState: %v", err)
	}

	want := []string{`azurerm_subnet.sub["app"]`, `azurerm_subnet.sub["db"]`}
	for i, address := range want {
		if s.Resources[i].Address != address {
			t.Errorf("posição %d = %q, quero %q", i, s.Resources[i].Address, address)
		}
	}
}

func TestInspectStateEntradaInvalida(t *testing.T) {
	tests := map[string]string{
		"não é JSON":           "isso não é json",
		"sem version":          `{"serial": 1, "resources": []}`,
		"versão não suportada": `{"version": 3, "serial": 1}`,
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := InspectState(strings.NewReader(input), "x"); err == nil {
				t.Error("erro = nil, quero falha")
			}
		})
	}
}

// downloader falso, para a inspeção por blob ser testada sem Azure.
type fakeDownloader struct {
	conteudo string
	err      error
	pedido   string
	fechado  bool
}

type fechavel struct {
	io.Reader
	dono *fakeDownloader
}

func (f *fechavel) Close() error {
	f.dono.fechado = true
	return nil
}

func (f *fakeDownloader) DownloadBlob(_ context.Context, name string) (io.ReadCloser, error) {
	f.pedido = name
	if f.err != nil {
		return nil, f.err
	}
	return &fechavel{Reader: strings.NewReader(f.conteudo), dono: f}, nil
}

func TestInspectStateBlob(t *testing.T) {
	conteudo, err := os.ReadFile("testdata/state_vazio.tfstate")
	if err != nil {
		t.Fatalf("lendo fixture: %v", err)
	}
	downloader := &fakeDownloader{conteudo: string(conteudo)}

	s, err := InspectStateBlob(context.Background(), downloader, "prod/app.tfstate")
	if err != nil {
		t.Fatalf("InspectStateBlob: %v", err)
	}

	if downloader.pedido != "prod/app.tfstate" {
		t.Errorf("blob pedido = %q", downloader.pedido)
	}
	if s.Path != "prod/app.tfstate" {
		t.Errorf("Path = %q", s.Path)
	}
	if !downloader.fechado {
		t.Error("o corpo do download não foi fechado")
	}
}

func TestInspectStateBlobPropagaErro(t *testing.T) {
	falha := errors.New("404 blob não existe")

	_, err := InspectStateBlob(context.Background(), &fakeDownloader{err: falha}, "x.tfstate")
	if !errors.Is(err, falha) {
		t.Errorf("erro = %v, quero %v", err, falha)
	}
}
