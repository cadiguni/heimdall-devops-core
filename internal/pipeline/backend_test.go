package pipeline

import (
	"os"
	"strings"
	"testing"
)

func linhasDoArquivo(t *testing.T, nome string) []string {
	t.Helper()

	b, err := os.ReadFile("testdata/" + nome)
	if err != nil {
		t.Fatalf("lendo fixture: %v", err)
	}
	return strings.Split(string(b), "\n")
}

// A fixture é o log real de um 'terraform init' da task TerraformTaskV4, que
// imprime a linha de comando completa antes de executar.
func TestExtractBackendDoLogReal(t *testing.T) {
	b := ExtractBackend(linhasDoArquivo(t, "pipeline_log_init.txt"))

	if !b.Complete() {
		t.Fatalf("backend incompleto: %+v", b)
	}
	if b.Account != "stexemploterraform" {
		t.Errorf("Account = %q", b.Account)
	}
	if b.Container != "meutime" {
		t.Errorf("Container = %q", b.Container)
	}
	if b.Key != "hml/aplicacaodemo01api.tfstate" {
		t.Errorf("Key = %q", b.Key)
	}
}

// "key=" também aparece dentro de "access_key=": a captura não pode confundir
// os dois, senão o state apontaria para a chave de acesso.
func TestExtractBackendNaoConfundeAccessKeyComKey(t *testing.T) {
	linha := "terraform init -backend-config=access_key=*** -backend-config=key=dev/app.tfstate"

	b := ExtractBackend([]string{linha})

	if b.Key != "dev/app.tfstate" {
		t.Errorf("Key = %q, quero dev/app.tfstate", b.Key)
	}
}

// Mesmo com o access_key em texto puro, ele não entra no Backend: a struct não
// tem onde guardar credencial.
func TestExtractBackendNuncaCapturaCredencial(t *testing.T) {
	linha := "terraform init -backend-config=access_key=CHAVE-EM-TEXTO-PURO " +
		"-backend-config=client_secret=SEGREDO " +
		"-backend-config=storage_account_name=st -backend-config=container_name=c -backend-config=key=k"

	b := ExtractBackend([]string{linha})

	for _, campo := range []string{b.Account, b.Container, b.Key} {
		if strings.Contains(campo, "CHAVE") || strings.Contains(campo, "SEGREDO") {
			t.Errorf("credencial entrou no backend: %+v", b)
		}
	}
	if b.Account != "st" || b.Container != "c" || b.Key != "k" {
		t.Errorf("backend = %+v", b)
	}
}

func TestExtractBackendComAspas(t *testing.T) {
	linha := `terraform init -backend-config="storage_account_name=st" -backend-config='container_name=c' -backend-config="key=dev/a.tfstate"`

	b := ExtractBackend([]string{linha})

	if !b.Complete() || b.Key != "dev/a.tfstate" {
		t.Errorf("backend = %+v", b)
	}
}

func TestExtractBackendIncompletoOuAusente(t *testing.T) {
	// Log sem init nenhum.
	if b := ExtractBackend(linhasDoArquivo(t, "pipeline_log_misto.txt")); b != (Backend{}) {
		t.Errorf("backend = %+v, quero vazio num log sem init", b)
	}

	// Parcial ainda ajuda: o que falta vem de perfil ou flag.
	parcial := ExtractBackend([]string{"terraform init -backend-config=container_name=c"})
	if parcial.Complete() {
		t.Error("Complete() = true sem account nem key")
	}
	if parcial.Container != "c" {
		t.Errorf("o que deu para extrair deveria ficar: %+v", parcial)
	}
}

// O ponto da extração: o comando sai pronto, apontando ao state da execução.
func TestComandoDeImportSaiCompleto(t *testing.T) {
	log, err := os.ReadFile("testdata/pipeline_log_init.txt")
	if err != nil {
		t.Fatalf("lendo init: %v", err)
	}
	falha, err := os.ReadFile("testdata/pipeline_log_misto.txt")
	if err != nil {
		t.Fatalf("lendo falhas: %v", err)
	}

	d, err := Diagnose(strings.NewReader(string(log)+string(falha)), "completo.log")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}

	if !d.Backend.Complete() {
		t.Fatalf("backend não foi extraído: %+v", d.Backend)
	}

	var comandos []string
	for _, f := range d.Findings {
		if f.Signature == "resource-already-exists" {
			comandos = append(comandos, f.Commands...)
		}
	}

	var completos int
	for _, c := range comandos {
		if strings.HasPrefix(c, "heimdall terraform import") {
			completos++
			for _, parte := range []string{
				"--account stexemploterraform",
				"--container meutime",
				"--key hml/aplicacaodemo01api.tfstate",
			} {
				if !strings.Contains(c, parte) {
					t.Errorf("comando sem %q: %s", parte, c)
				}
			}
		}
	}
	if completos != 2 {
		t.Errorf("comandos heimdall completos = %d, quero 2", completos)
	}
}

// Sem backend não se inventa comando: o achado fica com o terraform import cru.
func TestSemBackendNaoMontaComandoDoHeimdall(t *testing.T) {
	d, err := Diagnose(strings.NewReader(string(linhasDoArquivo(t, "pipeline_log_misto.txt")[0])+"\n"+
		`│ Error: a resource with the ID "/subscriptions/x/resourceGroups/y" already exists`+"\n"+
		"│   with azurerm_resource_group.rg,"), "")
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}

	for _, f := range d.Findings {
		for _, c := range f.Commands {
			if strings.HasPrefix(c, "heimdall terraform import") {
				t.Errorf("montou comando do heimdall sem saber o backend: %s", c)
			}
		}
	}
}
