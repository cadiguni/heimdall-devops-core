package pipeline

import (
	"fmt"
	"regexp"
	"strings"
)

// signature é uma falha reconhecível em log de pipeline.
type signature struct {
	id       string
	title    string
	severity Severity
	cause    string
	action   string
	re       *regexp.Regexp

	// enrich extrai detalhes da mensagem e monta comandos, olhando o contexto
	// em volta quando precisa. Opcional.
	enrich func(match []string, lines []string, idx int) (map[string]string, []string)
}

// withAddressRe casa o bloco de contexto que o Terraform imprime abaixo do
// erro, ex.: "  with azurerm_resource_group.rg[0],".
var withAddressRe = regexp.MustCompile(`^\s*(?:│)?\s*with\s+(\S+?),\s*$`)

// signatures é o catálogo, em ordem de precedência: a primeira que casa vence,
// então as específicas vêm antes das genéricas.
//
// O catálogo saiu de um corpus de 11 falhas reais de pipeline. Erros que são
// só consequência ("TerraformPlanFailed", "failed with exit code 1") ficam de
// fora de propósito: aparecem em toda falha e não apontam para causa nenhuma.
var signatures = []signature{
	{
		id:       "resource-already-exists",
		title:    "Recurso já existe no Azure, fora do state",
		severity: SeverityError,
		cause: "O recurso existe no Azure mas o state não o conhece, então o Terraform " +
			"tenta criar de novo e o Azure recusa.",
		action: "Importar o recurso para o state, ou remover do Azure antes de recriar.",
		re:     regexp.MustCompile(`a resource with the ID "([^"]+)" already exists`),
		enrich: func(match []string, lines []string, idx int) (map[string]string, []string) {
			details := map[string]string{"resource_id": match[1]}

			address := addressNear(lines, idx)
			if address == "" {
				return details, nil
			}
			details["address"] = address

			return details, []string{
				fmt.Sprintf("terraform import '%s' '%s'", address, match[1]),
			}
		},
	},
	{
		id:       "undeclared-variable",
		title:    "Variável passada na linha de comando sem estar declarada",
		severity: SeverityError,
		cause: "A pipeline passa -var para uma variável que o módulo raiz não declara. " +
			"Costuma ser variável nova no template da pipeline sem o bloco correspondente no código.",
		action: `Declarar o bloco 'variable "<nome>" {}' no módulo raiz, ou parar de passar o -var.`,
		re:     regexp.MustCompile(`A variable named "([^"]+)" was assigned on the command line`),
		enrich: func(match []string, _ []string, _ int) (map[string]string, []string) {
			return map[string]string{"variable": match[1]}, nil
		},
	},
	{
		id:       "cdn-custom-domain-cname",
		title:    "Custom domain do CDN não pode ser destruído: CNAME ainda aponta para ele",
		severity: SeverityError,
		cause: "O Azure recusa apagar o custom domain enquanto houver CNAME do DNS apontando " +
			"para o endpoint, direta ou indiretamente (prefixo cdnverify).",
		action: "Remover o registro CNAME do DNS e rodar o destroy de novo.",
		re:     regexp.MustCompile(`Cannot delete custom domain \\"([^\\]+)\\".*CNAMEd to CDN endpoint \\"([^\\]+)\\"`),
		enrich: func(match []string, _ []string, _ int) (map[string]string, []string) {
			return map[string]string{
				"custom_domain": match[1],
				"cdn_endpoint":  match[2],
			}, nil
		},
	},
	{
		id:       "resource-group-not-found",
		title:    "Resource Group não encontrado",
		severity: SeverityError,
		cause: "Um data source aponta para um Resource Group que não existe na subscription. " +
			"Acontece quando o RG é criado à mão fora da pipeline e ainda não foi criado no ambiente.",
		action: "Criar o RG na subscription, ou passar a gerenciá-lo por um resource no código.",
		re:     regexp.MustCompile(`Resource Group Name: \\"([^\\]+)\\"\)" was not found`),
		enrich: func(match []string, lines []string, idx int) (map[string]string, []string) {
			details := map[string]string{"resource_group": match[1]}
			if address := addressNear(lines, idx); address != "" {
				details["address"] = address
			}
			return details, nil
		},
	},
	{
		id:       "missing-pipeline-input",
		title:    "Input obrigatório da task não foi informado",
		severity: SeverityError,
		cause: "Falha da task do Azure DevOps, antes do Terraform rodar: um input obrigatório " +
			"ficou vazio no YAML ou veio de uma variável que não resolveu.",
		action: "Preencher o input na definição da pipeline e conferir se a variável que o alimenta existe no escopo.",
		re:     regexp.MustCompile(`##\[error\]Error: Input required: (\S+)`),
		enrich: func(match []string, _ []string, _ int) (map[string]string, []string) {
			return map[string]string{"input": match[1]}, nil
		},
	},
	{
		id:       "invalid-storage-account-name",
		title:    "Nome de Storage Account fora das regras do Azure",
		severity: SeverityError,
		cause: "O nome é montado por interpolação e o resultado estourou as regras do Azure: " +
			"3 a 24 caracteres, só letras minúsculas e números.",
		action: "Encurtar ou normalizar a interpolação que monta o nome.",
		re:     regexp.MustCompile(`name \("([^"]+)"\) can only consist of lowercase letters and numbers`),
		enrich: func(match []string, _ []string, _ int) (map[string]string, []string) {
			name := match[1]
			details := map[string]string{
				"name":   name,
				"length": fmt.Sprint(len(name)),
			}
			if violations := storageNameViolations(name); len(violations) > 0 {
				details["violations"] = strings.Join(violations, "; ")
			}
			return details, nil
		},
	},
	{
		id:       "provider-schema-mismatch",
		title:    "Código incompatível com o schema do provider",
		severity: SeverityError,
		cause: "O código usa um bloco ou argumento que a versão do provider em uso não tem " +
			"(ou não tem mais). Típico de major do AzureRM, onde argumentos viram recursos separados.",
		action: "Alinhar código e provider: migrar o código para o schema novo, ou fixar a versão anterior do provider até migrar.",
		re: regexp.MustCompile(`Blocks of type "([^"]+)" are not expected here` +
			`|An argument named "([^"]+)" is not expected here` +
			`|The argument "([^"]+)" is required, but no definition was`),
		enrich: func(match []string, _ []string, _ int) (map[string]string, []string) {
			return map[string]string{"symbol": firstNonEmpty(match)}, nil
		},
	},
	{
		id:       "parent-resource-not-found",
		title:    "Recurso pai não existe no Azure",
		severity: SeverityError,
		cause: "A operação depende de um recurso pai que não está lá. Costuma ser ordem de " +
			"criação, ou o pai ter sido removido por fora do Terraform.",
		action: "Conferir se o recurso pai existe e se há dependência declarada garantindo a ordem.",
		re:     regexp.MustCompile(`ParentResourceNotFound.*parent resource '([^']+)' could not be found`),
		enrich: func(match []string, _ []string, _ int) (map[string]string, []string) {
			return map[string]string{"parent_resource": match[1]}, nil
		},
	},
	{
		id:       "azure-resource-not-found",
		title:    "Recurso consultado não existe no Azure",
		severity: SeverityError,
		cause: "Um data source consultou um recurso que não existe. Pode ser nome errado, ambiente " +
			"errado, ou atraso entre a criação e o recurso ficar visível na API.",
		action: "Conferir nome e ambiente. Se o recurso foi criado no mesmo apply, declarar depends_on em vez de confiar no tempo.",
		re:     regexp.MustCompile(`Error: the (.+?) \(Subscription: "`),
		enrich: func(match []string, lines []string, idx int) (map[string]string, []string) {
			details := map[string]string{"resource_type": match[1]}
			if address := addressNear(lines, idx); address != "" {
				details["address"] = address
			}
			return details, nil
		},
	},
	{
		id:       "deprecated-argument",
		title:    "Argumento depreciado no provider",
		severity: SeverityWarning,
		cause: "Ainda funciona nesta versão do provider, mas sai no próximo major — e aí vira " +
			"o erro de schema incompatível.",
		action: "Migrar antes do bump de major do provider.",
		re:     regexp.MustCompile(`Warning: Argument is deprecated`),
		enrich: func(_ []string, lines []string, idx int) (map[string]string, []string) {
			if address := addressNear(lines, idx); address != "" {
				return map[string]string{"address": address}, nil
			}
			return nil, nil
		},
	},
}

// storageNameViolations diz quais regras do Azure o nome quebra. O log só diz
// que o nome é inválido; saber qual das regras foi violada poupa a conferência
// manual, já que o nome costuma vir de interpolação.
func storageNameViolations(name string) []string {
	var violations []string

	if len(name) < 3 || len(name) > 24 {
		violations = append(violations, fmt.Sprintf("tem %d caracteres, fora da faixa de 3 a 24", len(name)))
	}
	if strings.ToLower(name) != name {
		violations = append(violations, "tem letra maiúscula")
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && (r < 'A' || r > 'Z') {
			violations = append(violations, fmt.Sprintf("tem caractere inválido %q", r))
			break
		}
	}

	return violations
}
