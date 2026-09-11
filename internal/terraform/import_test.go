package terraform

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

// fakeStateClient junta listagem e download: é o que o preflight precisa.
type fakeStateClient struct {
	blobs    []azure.Blob
	conteudo string
	listErr  error
	downErr  error
}

func (f *fakeStateClient) ListBlobs(_ context.Context, prefix string) ([]azure.Blob, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	var out []azure.Blob
	for _, b := range f.blobs {
		if strings.HasPrefix(b.Name, prefix) {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeStateClient) DownloadBlob(_ context.Context, _ string) (io.ReadCloser, error) {
	if f.downErr != nil {
		return nil, f.downErr
	}
	return io.NopCloser(strings.NewReader(f.conteudo)), nil
}

const stateComRG = `{
  "version": 4, "terraform_version": "1.9.8", "serial": 12, "lineage": "abc",
  "outputs": {},
  "resources": [
    {"mode":"managed","type":"azurerm_resource_group","name":"rg","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{"index_key":0}]},
    {"mode":"managed","type":"azurerm_storage_account","name":"stg","provider":"provider[\"registry.terraform.io/hashicorp/azurerm\"]",
     "instances":[{}]}
  ]
}`

const stateVazio = `{"version":4,"terraform_version":"1.9.8","serial":1,"lineage":"abc","outputs":{},"resources":[]}`

func pedido(address string) ImportRequest {
	return ImportRequest{
		Address:      address,
		ResourceID:   "/subscriptions/x/resourceGroups/app-dev-rg",
		Account:      "stterraform",
		Container:    "time1",
		Key:          "dev/app.tfstate",
		ContainerURL: "https://stterraform.blob.core.windows.net/time1",
	}
}

func preflight(t *testing.T, client StateClient, req ImportRequest) *ImportPreflight {
	t.Helper()

	p, err := PreflightImport(context.Background(), client, req)
	if err != nil {
		t.Fatalf("PreflightImport: %v", err)
	}
	return p
}

func statusDe(p *ImportPreflight, name string) CheckStatus {
	for _, c := range p.Checks {
		if c.Name == name {
			return c.Status
		}
	}
	return ""
}

func TestPreflightImportCaminhoFeliz(t *testing.T) {
	client := &fakeStateClient{
		blobs:    []azure.Blob{{Name: "dev/app.tfstate", SizeBytes: 4096, LeaseState: "available"}},
		conteudo: stateComRG,
	}

	p := preflight(t, client, pedido("azurerm_key_vault.kv"))

	if p.Blocked() {
		t.Fatalf("bloqueado sem motivo: %+v", p.Checks)
	}
	for _, name := range []string{"state existe", "state destravado", "endereço livre", "conteúdo do state"} {
		if got := statusDe(p, name); got != CheckOK {
			t.Errorf("%s = %q, quero ok", name, got)
		}
	}
	if p.Environment != "dev" {
		t.Errorf("Environment = %q, quero dev", p.Environment)
	}
}

// O modo de falha mais comum: o recurso já está no state e alguém tenta
// importar de novo.
func TestPreflightImportBloqueiaEnderecoOcupado(t *testing.T) {
	client := &fakeStateClient{
		blobs:    []azure.Blob{{Name: "dev/app.tfstate", SizeBytes: 4096, LeaseState: "available"}},
		conteudo: stateComRG,
	}

	p := preflight(t, client, pedido("azurerm_storage_account.stg"))

	if !p.Blocked() {
		t.Fatal("deveria bloquear: o endereço já está no state")
	}
	if got := statusDe(p, "endereço livre"); got != CheckFail {
		t.Errorf("endereço livre = %q, quero fail", got)
	}
}

// Endereço com índice precisa bater exatamente: rg[0] está no state, rg[1] não.
func TestPreflightImportComparaEnderecoComIndice(t *testing.T) {
	client := &fakeStateClient{
		blobs:    []azure.Blob{{Name: "dev/app.tfstate", SizeBytes: 4096, LeaseState: "available"}},
		conteudo: stateComRG,
	}

	if p := preflight(t, client, pedido("azurerm_resource_group.rg[0]")); !p.Blocked() {
		t.Error("rg[0] está no state, deveria bloquear")
	}
	if p := preflight(t, client, pedido("azurerm_resource_group.rg[1]")); p.Blocked() {
		t.Error("rg[1] não está no state, não deveria bloquear")
	}
}

func TestPreflightImportBloqueiaStateInexistente(t *testing.T) {
	client := &fakeStateClient{
		blobs: []azure.Blob{{Name: "dev/outro.tfstate", SizeBytes: 4096}},
	}

	p := preflight(t, client, pedido("azurerm_key_vault.kv"))

	if !p.Blocked() {
		t.Fatal("deveria bloquear: o state não existe no container")
	}
	if got := statusDe(p, "state existe"); got != CheckFail {
		t.Errorf("state existe = %q, quero fail", got)
	}
	// Sem state não adianta tentar as outras checagens.
	if len(p.Checks) != 1 {
		t.Errorf("checagens = %d, quero parar na primeira falha: %+v", len(p.Checks), p.Checks)
	}
}

func TestPreflightImportBloqueiaStateTravado(t *testing.T) {
	client := &fakeStateClient{
		blobs: []azure.Blob{{
			Name: "dev/app.tfstate", SizeBytes: 4096, LeaseState: "leased",
			Metadata: lockMetadata(t, lockBody),
		}},
		conteudo: stateComRG,
	}

	p := preflight(t, client, pedido("azurerm_key_vault.kv"))

	if !p.Blocked() {
		t.Fatal("deveria bloquear: o state está travado")
	}
	if got := statusDe(p, "state destravado"); got != CheckFail {
		t.Errorf("state destravado = %q, quero fail", got)
	}
	// Quem travou ajuda a decidir se é esperar ou destravar.
	var detail string
	for _, c := range p.Checks {
		if c.Name == "state destravado" {
			detail = c.Detail
		}
	}
	if !strings.Contains(detail, "lucas@vm-build") {
		t.Errorf("detalhe não diz quem travou: %q", detail)
	}
}

// State vazio não bloqueia — importar para um state novo é legítimo —, mas
// merece aviso, porque também é o sintoma de backend errado.
func TestPreflightImportAvisaStateVazio(t *testing.T) {
	client := &fakeStateClient{
		blobs:    []azure.Blob{{Name: "dev/app.tfstate", SizeBytes: 181, LeaseState: "available"}},
		conteudo: stateVazio,
	}

	p := preflight(t, client, pedido("azurerm_key_vault.kv"))

	if p.Blocked() {
		t.Errorf("state vazio não deveria bloquear: %+v", p.Checks)
	}
	if got := statusDe(p, "conteúdo do state"); got != CheckWarn {
		t.Errorf("conteúdo do state = %q, quero warn", got)
	}
}

func TestPreflightImportRecusaEnderecoInvalido(t *testing.T) {
	client := &fakeStateClient{
		blobs:    []azure.Blob{{Name: "dev/app.tfstate", SizeBytes: 4096}},
		conteudo: stateComRG,
	}

	casos := map[string]string{
		"vazio":       "",
		"sem ponto":   "azurerm_resource_group",
		"data source": "data.azurerm_resource_group.rg",
	}

	for name, address := range casos {
		t.Run(name, func(t *testing.T) {
			req := pedido(address)
			if _, err := PreflightImport(context.Background(), client, req); err == nil {
				t.Error("erro = nil, quero recusa")
			}
		})
	}
}

// O Git Bash converte argumento iniciado em "/" em caminho do Windows, e todo
// id do Azure começa com "/subscriptions/". Sem essa checagem, o erro só
// apareceria depois, vindo do provider, sem explicar a causa.
func TestPreflightImportDetectaIDConvertidoPeloShell(t *testing.T) {
	client := &fakeStateClient{
		blobs:    []azure.Blob{{Name: "dev/app.tfstate", SizeBytes: 4096, LeaseState: "available"}},
		conteudo: stateComRG,
	}

	req := pedido("azurerm_key_vault.kv")
	req.ResourceID = "C:/Program Files/Git/subscriptions/x/resourceGroups/app-dev-rg"

	_, err := PreflightImport(context.Background(), client, req)

	if err == nil {
		t.Fatal("erro = nil, quero recusar o id estragado")
	}
	if !strings.Contains(err.Error(), "MSYS_NO_PATHCONV") {
		t.Errorf("erro não diz como contornar: %v", err)
	}
}

func TestCheckResourceID(t *testing.T) {
	validos := []string{
		"/subscriptions/x/resourceGroups/rg",
		"/subscriptions/x/resourceGroups/rg/providers/Microsoft.KeyVault/vaults/kv",
		// Providers que não usam id em forma de caminho continuam valendo.
		"i-0123456789abcdef",
		"minha-conta/meu-repo",
	}
	for _, id := range validos {
		if err := checkResourceID(id); err != nil {
			t.Errorf("checkResourceID(%q) = %v, quero nil", id, err)
		}
	}

	invalidos := []string{
		"",
		"C:/Program Files/Git/subscriptions/x/resourceGroups/rg",
		"D:/msys64/resourceGroups/rg",
	}
	for _, id := range invalidos {
		if err := checkResourceID(id); err == nil {
			t.Errorf("checkResourceID(%q) = nil, quero erro", id)
		}
	}
}

func TestPreflightImportPropagaErroDeListagem(t *testing.T) {
	falha := errors.New("403 sem permissão")

	_, err := PreflightImport(context.Background(), &fakeStateClient{listErr: falha}, pedido("azurerm_key_vault.kv"))
	if !errors.Is(err, falha) {
		t.Errorf("erro = %v, quero %v", err, falha)
	}
}

func TestPreflightImportMontaComandos(t *testing.T) {
	client := &fakeStateClient{
		blobs:    []azure.Blob{{Name: "dev/app.tfstate", SizeBytes: 4096, LeaseState: "available"}},
		conteudo: stateComRG,
	}

	p := preflight(t, client, pedido("azurerm_key_vault.kv"))

	if len(p.Commands) != 2 {
		t.Fatalf("comandos = %+v, quero init e import", p.Commands)
	}
	if !strings.Contains(p.Commands[0], "key=dev/app.tfstate") {
		t.Errorf("init sem a chave do backend: %q", p.Commands[0])
	}
	if !strings.Contains(p.Commands[0], "use_azuread_auth=true") {
		t.Errorf("init sem autenticação por Entra ID: %q", p.Commands[0])
	}
	want := `terraform import 'azurerm_key_vault.kv' '/subscriptions/x/resourceGroups/app-dev-rg'`
	if p.Commands[1] != want {
		t.Errorf("import = %q, quero %q", p.Commands[1], want)
	}
}

func TestWriteImportPreflightDryRun(t *testing.T) {
	client := &fakeStateClient{
		blobs:    []azure.Blob{{Name: "dev/app.tfstate", SizeBytes: 4096, LeaseState: "available"}},
		conteudo: stateComRG,
	}

	var buf bytes.Buffer
	if err := WriteImportPreflight(&buf, preflight(t, client, pedido("azurerm_key_vault.kv")), false); err != nil {
		t.Fatalf("WriteImportPreflight: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"https://stterraform.blob.core.windows.net/time1", // o alvo vem primeiro
		"dev/app.tfstate",
		"ambiente   dev",
		"Dry-run: nada foi executado.",
		"terraform import 'azurerm_key_vault.kv'",
		"--apply",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("saída não contém %q:\n%s", want, out)
		}
	}
}

// Bloqueado não pode sugerir comando: seria convidar a contornar a verificação.
func TestWriteImportPreflightBloqueadoNaoSugereComando(t *testing.T) {
	client := &fakeStateClient{
		blobs:    []azure.Blob{{Name: "dev/app.tfstate", SizeBytes: 4096, LeaseState: "available"}},
		conteudo: stateComRG,
	}

	var buf bytes.Buffer
	if err := WriteImportPreflight(&buf, preflight(t, client, pedido("azurerm_storage_account.stg")), false); err != nil {
		t.Fatalf("WriteImportPreflight: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Import bloqueado. Nada foi executado.") {
		t.Errorf("faltou dizer que bloqueou:\n%s", out)
	}
	if strings.Contains(out, "terraform import '") {
		t.Errorf("não pode sugerir o comando com o import bloqueado:\n%s", out)
	}
}

func TestWriteImportPreflightPropagaErroDeEscrita(t *testing.T) {
	client := &fakeStateClient{
		blobs:    []azure.Blob{{Name: "dev/app.tfstate", SizeBytes: 4096, LeaseState: "available"}},
		conteudo: stateComRG,
	}

	p := preflight(t, client, pedido("azurerm_key_vault.kv"))
	if err := WriteImportPreflight(falhaNoWrite{}, p, false); err == nil {
		t.Error("WriteImportPreflight não propagou o erro de escrita")
	}
}
