package pipeline

import (
	"regexp"
	"strings"
)

// Backend é o backend que a execução usou, extraído do log do 'terraform init'.
//
// Só os três campos que identificam o state. Os outros parâmetros do
// -backend-config (access_key, client_id, client_secret) são material de
// credencial e não entram aqui nem quando o log os traz em texto puro.
type Backend struct {
	Account   string `json:"account,omitempty"`
	Container string `json:"container,omitempty"`
	Key       string `json:"key,omitempty"`
}

// Complete informa se há o suficiente para apontar um comando ao state.
func (b Backend) Complete() bool {
	return b.Account != "" && b.Container != "" && b.Key != ""
}

// backendParams casa um parâmetro do -backend-config na linha de comando que a
// task do Azure DevOps imprime antes de rodar o terraform.
//
// O valor vai até o próximo espaço: nenhum dos três campos extraídos admite
// espaço, e parar no espaço evita engolir o parâmetro seguinte.
var backendParams = map[string]*regexp.Regexp{
	"account":   regexp.MustCompile(`-backend-config=["']?storage_account_name=([^\s"']+)`),
	"container": regexp.MustCompile(`-backend-config=["']?container_name=([^\s"']+)`),
	"key":       regexp.MustCompile(`-backend-config=["']?key=([^\s"']+)`),
}

// ExtractBackend procura, no log, o backend com que a execução foi
// inicializada.
//
// A task TerraformTaskV4 do Azure DevOps imprime a linha de comando completa
// antes de executar, e é de lá que sai o state — sem isso, quem lê o log de
// uma falha sabe o que deu errado mas não contra qual state corrigir.
//
// Devolve o que conseguir: um backend incompleto ainda ajuda, e o campo que
// faltar vem de perfil ou de flag.
func ExtractBackend(lines []string) Backend {
	var b Backend

	for _, line := range lines {
		if !strings.Contains(line, "-backend-config=") {
			continue
		}

		if b.Account == "" {
			b.Account = firstSubmatch(backendParams["account"], line)
		}
		if b.Container == "" {
			b.Container = firstSubmatch(backendParams["container"], line)
		}
		if b.Key == "" {
			// "key=" também casaria dentro de "access_key=", então a captura
			// exige que o parâmetro comece exatamente em key.
			b.Key = firstSubmatch(backendParams["key"], line)
		}

		if b.Complete() {
			break
		}
	}

	return b
}

func firstSubmatch(re *regexp.Regexp, line string) string {
	if m := re.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

// applyBackendToCommands reescreve os comandos sugeridos para apontar ao state
// que a execução usou.
//
// É o ponto da extração: sem backend, o achado de "recurso já existe" dá o
// endereço e o ID mas deixa a pessoa descobrir contra qual state corrigir. Com
// ele, o comando sai completo e não sobra passo manual nenhum.
func applyBackendToCommands(findings []Finding, b Backend) {
	if !b.Complete() {
		return
	}

	for i := range findings {
		address := findings[i].Details["address"]
		resourceID := findings[i].Details["resource_id"]
		if address == "" || resourceID == "" {
			continue
		}

		findings[i].Commands = append(findings[i].Commands, heimdallImportCommand(address, resourceID, b))
	}
}

func heimdallImportCommand(address, resourceID string, b Backend) string {
	return "heimdall terraform import '" + address + "' '" + resourceID + "'" +
		" --account " + b.Account +
		" --container " + b.Container +
		" --key " + b.Key
}
