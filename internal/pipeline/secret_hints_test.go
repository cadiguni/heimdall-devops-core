package pipeline

import (
	"strings"
	"testing"
)

func TestLooksLikeSecret(t *testing.T) {
	// O primeiro caso é o real: apareceu em texto puro ao lado de um
	// "TerraformAccessKey" corretamente marcado.
	indicamSegredo := []string{
		"storage-AccessKey",
		"TerraformAccessKey",
		"ARM_CLIENT_SECRET",
		"dbPassword",
		"senha_banco",
		"githubToken",
		"sonarToken",
		"API_KEY",
		"connectionString",
		"AzureWebJobsStorage-ConnectionString",
		"registryCredential",
		"PAT",
		"azure-pat",
		"pwd",
		"sasUrl",
		"sshPrivateKey",
		"certificatePassword",
		"publishProfile",
	}
	for _, nome := range indicamSegredo {
		if !looksLikeSecret(nome) {
			t.Errorf("looksLikeSecret(%q) = false, quero true", nome)
		}
	}

	// Falsos positivos que a heurística precisa evitar. "path" contém "pat" e
	// "sasl" contém "sas": é por isso que as palavras curtas só valem como
	// segmento inteiro.
	naoIndicam := []string{
		"AMBIENTE",
		"backendKey", // "key" é segmento, mas ver o caso abaixo
		"pathPrefix",
		"artifactPath",
		"saslMechanism",
		"patchLevel",
		"databaseName",
		"resourceGroup",
		"imageTag",
		"dotnetVersion",
	}
	for _, nome := range naoIndicam {
		if nome == "backendKey" {
			continue // tratado no teste seguinte
		}
		if looksLikeSecret(nome) {
			t.Errorf("looksLikeSecret(%q) = true, quero false", nome)
		}
	}
}

// "key" sozinho é ambíguo: pega "backendKey", que não é segredo. A heurística
// erra para o lado de apontar, e o relatório diz que é suspeita e não conclusão.
func TestLooksLikeSecretAssumeFalsoPositivoEmKey(t *testing.T) {
	if !looksLikeSecret("backendKey") {
		t.Error("o segmento 'key' deveria disparar a suspeita, mesmo com falso positivo")
	}
}

func TestSplitIdentifier(t *testing.T) {
	tests := map[string][]string{
		"storage-AccessKey": {"storage", "access", "key"},
		"pathPrefix":        {"path", "prefix"},
		"ARM_CLIENT_SECRET": {"arm", "client", "secret"},
		"GVdasaAPIKey":      {"g", "vdasa", "api", "key"},
		"simples":           {"simples"},
	}

	for entrada, want := range tests {
		got := splitIdentifier(entrada)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("splitIdentifier(%q) = %v, quero %v", entrada, got, want)
		}
	}
}

func TestSummarizeContaSuspeitas(t *testing.T) {
	d := encontrar(t, "terraform-dev", false)

	// backendKey dispara a suspeita; AMBIENTE não; ARM_CLIENT_SECRET já está
	// marcada, então não conta.
	if d.Unmarked != 1 {
		t.Errorf("Unmarked = %d, quero 1: %+v", d.Unmarked, d.Variables)
	}

	porNome := map[string]Variable{}
	for _, v := range d.Variables {
		porNome[v.Name] = v
	}
	if !porNome["backendKey"].LooksSecret {
		t.Error("backendKey deveria estar marcada como suspeita")
	}
	if porNome["ARM_CLIENT_SECRET"].LooksSecret {
		t.Error("variável já marcada como secreta não é suspeita")
	}
	if porNome["AMBIENTE"].LooksSecret {
		t.Error("AMBIENTE não deveria ser suspeita")
	}
}

// Os dois falsos positivos que apareceram na organização real.
func TestLooksLikeSecretDescartaFalsosPositivosConhecidos(t *testing.T) {
	naoSaoSegredo := map[string]string{
		"VapidDetails.PublicKey": "chave pública existe para ser pública",
		"publicCertificate":      "certificado público",
		"docker_expiration_pat":  "guarda a data de validade do PAT, não o PAT",
		"expires_date_pat":       "idem",
		"tokenValidade":          "idem, em português",
	}

	for nome, porque := range naoSaoSegredo {
		if looksLikeSecret(nome) {
			t.Errorf("looksLikeSecret(%q) = true: %s", nome, porque)
		}
	}

	// A exclusão não pode engolir o caso legítimo ao lado.
	if !looksLikeSecret("VapidDetails.PrivateKey") {
		t.Error("chave privada continua sendo segredo")
	}
	if !looksLikeSecret("docker_pat") {
		t.Error("o PAT em si continua sendo segredo")
	}
}
