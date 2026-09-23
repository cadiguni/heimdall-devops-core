package main

import "runtime/debug"

// version pode ser fixada no build com -ldflags "-X main.version=...".
// Deixada vazia, a versão é resolvida do BuildInfo.
var version string

// Rótulos para os casos em que o BuildInfo não traz uma versão de verdade.
const (
	versionDevel           = "(devel)"
	versionDesconhecida    = "desconhecida"
	versionDesenvolvimento = "desenvolvimento"
)

// resolveVersion descobre a versão do binário.
//
// Três origens, em ordem de precedência:
//
//   - -ldflags, quando alguém fixa explicitamente;
//   - BuildInfo, que o Go preenche sozinho — "v0.1.0" quando o binário veio de
//     'go install módulo@v0.1.0', e um pseudo-version com commit e data quando
//     veio de 'go build' dentro do repositório;
//   - um rótulo, quando não há nenhum dos dois.
//
// O sufixo "+dirty" que aparece no pseudo-version vem do próprio Go e significa
// build feito com alteração não commitada. É a resposta para "por que meu
// binário não tem o comando que acabei de escrever", que já custou tempo neste
// projeto quando o 'go install @latest' entregou um binário velho.
func resolveVersion() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return versionDesconhecida
	}

	// "(devel)" é o que o 'go run' reporta: não passa por VCS e não vira
	// pseudo-version.
	if info.Main.Version == versionDevel {
		return versionDesenvolvimento
	}

	return info.Main.Version
}
