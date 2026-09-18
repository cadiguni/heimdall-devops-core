package pipeline

import (
	"strings"
	"unicode"
)

// Nomes que indicam segredo por si sós. São longos o bastante para casar por
// substring sem falso positivo.
var secretSubstrings = []string{
	"password", "passwd", "senha",
	"secret",
	"token",
	"credential",
	"apikey", "accesskey", "privatekey", "publishprofile",
	"connectionstring",
	"certificate",
}

// Palavras curtas demais para casar por substring: "pat" está dentro de "path"
// e "patch", "sas" dentro de "sasl". Estas só valem como segmento inteiro do
// nome.
var secretSegments = map[string]bool{
	"pat": true,
	"pwd": true,
	"sas": true,
	"key": true,
}

// Palavras que desmentem a suspeita, mesmo quando o resto do nome a levanta.
//
// Ambas saíram de falsos positivos reais numa organização com 63 grupos:
// "VapidDetails.PublicKey" é chave pública, que existe para ser pública; e
// "docker_expiration_pat" guarda a data de validade de um PAT, não o PAT.
var notSecretSubstrings = []string{
	"public",
	"expir", "validade", "vencimento",
}

// looksLikeSecret diz se o nome da variável indica que ela guarda um segredo.
//
// Serve para apontar variável que ninguém marcou como secreta e deveria ter
// marcado — foi assim que apareceu um "storage-AccessKey" em texto puro ao lado
// de um "TerraformAccessKey" corretamente marcado.
//
// É heurística sobre nome: erra para os dois lados, e por isso o relatório fala
// em "parece segredo", não em "é segredo".
func looksLikeSecret(name string) bool {
	lower := strings.ToLower(name)

	for _, s := range notSecretSubstrings {
		if strings.Contains(lower, s) {
			return false
		}
	}

	for _, s := range secretSubstrings {
		if strings.Contains(lower, s) {
			return true
		}
	}

	for _, segment := range splitIdentifier(name) {
		if secretSegments[segment] {
			return true
		}
	}

	return false
}

// splitIdentifier quebra o nome em segmentos minúsculos, tanto pelos
// separadores comuns quanto pelas maiúsculas do camelCase.
//
// "storage-AccessKey" vira [storage, access, key]; "myPathPrefix" vira
// [my, path, prefix], e por isso "path" não é confundido com "pat".
func splitIdentifier(name string) []string {
	var (
		segments []string
		atual    strings.Builder
	)

	fecha := func() {
		if atual.Len() > 0 {
			segments = append(segments, strings.ToLower(atual.String()))
			atual.Reset()
		}
	}

	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '-' || r == '_' || r == '.' || r == ' ' || r == '/':
			fecha()
		case unicode.IsUpper(r):
			// Maiúscula começa segmento novo, exceto dentro de uma sigla:
			// em "GVdasaAPIKey" o corte certo é antes do "K" de Key.
			anteriorMinuscula := i > 0 && unicode.IsLower(runes[i-1])
			proximaMinuscula := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if anteriorMinuscula || (i > 0 && unicode.IsUpper(runes[i-1]) && proximaMinuscula) {
				fecha()
			}
			atual.WriteRune(r)
		default:
			atual.WriteRune(r)
		}
	}
	fecha()

	return segments
}
