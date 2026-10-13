package pipeline

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// RefSource diz de onde veio o achado. Os três lugares em que uma assinatura
// sobrevive depois de um move de recursos.
type RefSource string

const (
	// SourceConnection é uma service connection do Azure DevOps.
	SourceConnection RefSource = "service-connection"

	// SourceVariableGroup é uma variável de Variable Group — a library.
	SourceVariableGroup RefSource = "variable-group"

	// SourceRepo é um arquivo do repositório.
	SourceRepo RefSource = "repo"
)

// Reference é um lugar onde a assinatura procurada aparece.
//
// Não existe campo de valor aqui, e é de propósito: o valor de uma variável
// pode ser uma connection string com a assinatura no meio, e a saída de um
// comando acaba em log de pipeline. O achado é o endereço do que precisa mudar,
// não o conteúdo.
type Reference struct {
	Source RefSource `json:"source"`

	// Where é o grupo, a conexão ou o arquivo.
	Where string `json:"where"`

	// What é a variável, o campo ou o identificador da linha. Vazio quando a
	// linha não tem um identificador reconhecível.
	What string `json:"what,omitempty"`

	// Line só existe em achado de repositório.
	Line int `json:"line,omitempty"`

	Detail string `json:"detail,omitempty"`
}

// RepoFile é um arquivo do repositório, já lido.
type RepoFile struct {
	Path    string
	Content string
}

// ReferenceReport é o resultado da varredura.
type ReferenceReport struct {
	Subscription string `json:"subscription"`
	Project      string `json:"project,omitempty"`
	RepoDir      string `json:"repo_dir,omitempty"`

	// Scanned conta o que foi olhado, para a ausência de achado significar
	// algo. "Nenhuma referência" em cima de zero grupos varridos não é
	// resposta.
	ScannedConnections int `json:"scanned_connections"`
	ScannedGroups      int `json:"scanned_groups"`
	ScannedFiles       int `json:"scanned_files"`

	References []Reference `json:"references"`

	// SecretBlind são as variáveis secretas, cujo valor a API devolve como
	// null. Não dá para saber se citam a assinatura, e o relatório diz isso em
	// vez de contá-las como limpas.
	SecretBlind []Reference `json:"secret_blind"`

	// Notes são limites da varredura que mudam a leitura do resultado.
	Notes []string `json:"notes,omitempty"`
}

// Found informa se algo foi achado.
func (r *ReferenceReport) Found() bool { return len(r.References) > 0 }

// FromSource filtra os achados de uma origem.
func (r *ReferenceReport) FromSource(s RefSource) []Reference {
	var out []Reference
	for _, ref := range r.References {
		if ref.Source == s {
			out = append(out, ref)
		}
	}
	return out
}

// ReferenceRequest descreve a varredura.
type ReferenceRequest struct {
	// Subscription é a assinatura procurada. É o GUID antigo, o que saiu.
	Subscription string

	// Names são outros textos a procurar — nome de service connection, nome de
	// assinatura. Opcional.
	Names []string

	Connections []Connection
	Groups      []VariableGroupDetail
	Files       []RepoFile
}

// FindReferences procura a assinatura nas três origens.
//
// Função pura: quem lê a API e o disco é o comando. Isso é o que permite testar
// o relatório inteiro sem organização e sem repositório.
func FindReferences(req ReferenceRequest) *ReferenceReport {
	report := &ReferenceReport{
		Subscription:       req.Subscription,
		ScannedConnections: len(req.Connections),
		ScannedGroups:      len(req.Groups),
		ScannedFiles:       len(req.Files),
		References:         []Reference{},
		SecretBlind:        []Reference{},
	}

	alvos := append([]string{req.Subscription}, req.Names...)

	findInConnections(report, req.Connections, req.Subscription, alvos)
	findInGroups(report, req.Groups, alvos)
	findInFiles(report, req.Files, alvos)
	noteDanglingConnection(report)

	return report
}

// noteDanglingConnection cruza as duas origens do Azure DevOps.
//
// Variável de library citando a assinatura enquanto nenhuma service connection
// do projeto aponta para ela é um descasamento, e apareceu na primeira execução
// contra uma organização real: nove grupos apontavam para uma conexão que não
// está na listagem do projeto. Não dá para dizer por quê — pode estar em outro
// projeto, ter sido apagada, ou não vir nesta listagem —, e justamente por isso
// a nota existe em vez de um veredito.
func noteDanglingConnection(report *ReferenceReport) {
	naLibrary := len(report.FromSource(SourceVariableGroup))
	if naLibrary == 0 || report.ScannedConnections == 0 {
		return
	}
	if len(report.FromSource(SourceConnection)) > 0 {
		return
	}

	report.Notes = append(report.Notes, fmt.Sprintf(
		"%d variável(is) de library citam a assinatura, mas nenhuma das %d service connections "+
			"do projeto aponta para ela. A conexão citada pode estar em outro projeto, ter sido "+
			"apagada, ou não aparecer nesta listagem — vale olhar antes de trocar o valor",
		naLibrary, report.ScannedConnections))
}

// findInConnections acha as conexões apontadas para a assinatura.
//
// Aqui a comparação é no campo próprio, não no texto: a conexão declara a
// assinatura em data.subscriptionId, e isso é mais confiável que procurar o
// GUID no nome.
func findInConnections(report *ReferenceReport, conns []Connection, subscription string, alvos []string) {
	for _, c := range conns {
		switch {
		case subscription != "" && strings.EqualFold(c.SubscriptionID, subscription):
			detalhe := "aponta para a assinatura em data.subscriptionId"
			if c.SubscriptionName != "" {
				detalhe += fmt.Sprintf(" (%s)", c.SubscriptionName)
			}
			report.References = append(report.References, Reference{
				Source: SourceConnection,
				Where:  c.Name,
				What:   "subscriptionId",
				Detail: detalhe,
			})

		case matchesAny(c.Name, alvos):
			// O nome citar a assinatura não quer dizer que a conexão aponte
			// para ela — pode ser nome desatualizado, que é justamente um
			// problema a relatar.
			report.References = append(report.References, Reference{
				Source: SourceConnection,
				Where:  c.Name,
				What:   "nome",
				Detail: "o nome cita a assinatura, mas data.subscriptionId não é ela",
			})
		}
	}
}

// findInGroups acha as variáveis de library que citam a assinatura.
func findInGroups(report *ReferenceReport, groups []VariableGroupDetail, alvos []string) {
	for _, g := range groups {
		for _, v := range g.Variables {
			// Variável secreta vem com valor null da API. Não dá para procurar
			// dentro, e omitir seria dizer que está limpa.
			if v.Secret {
				report.SecretBlind = append(report.SecretBlind, Reference{
					Source: SourceVariableGroup,
					Where:  g.Name,
					What:   v.Name,
					Detail: "secreta: a API devolve null no valor, então não dá para verificar",
				})
				continue
			}

			switch {
			case matchesAny(v.Value, alvos):
				report.References = append(report.References, Reference{
					Source: SourceVariableGroup,
					Where:  g.Name,
					What:   v.Name,
					Detail: "o valor cita a assinatura",
				})
			case matchesAny(v.Name, alvos):
				report.References = append(report.References, Reference{
					Source: SourceVariableGroup,
					Where:  g.Name,
					What:   v.Name,
					Detail: "o nome da variável cita a assinatura",
				})
			}
		}
	}
}

// atribuicao casa o identificador à esquerda de = ou :, que é como a
// assinatura aparece em tfvars, HCL e YAML.
var atribuicao = regexp.MustCompile(`([A-Za-z_][\w.\-]*)\s*[:=]`)

// findInFiles acha as linhas de repositório que citam a assinatura.
//
// A linha não é copiada para o relatório: em tfvars ela pode ser uma connection
// string, e o relatório precisa poder ir para log de pipeline. Vai o arquivo, a
// linha e o identificador — o suficiente para abrir no editor.
func findInFiles(report *ReferenceReport, files []RepoFile, alvos []string) {
	for _, f := range files {
		scanner := bufio.NewScanner(strings.NewReader(f.Content))
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		numero := 0
		for scanner.Scan() {
			numero++
			linha := scanner.Text()
			if !matchesAny(linha, alvos) {
				continue
			}

			ref := Reference{Source: SourceRepo, Where: f.Path, Line: numero}
			if m := atribuicao.FindStringSubmatch(linha); m != nil {
				ref.What = m[1]
			}
			if strings.HasPrefix(strings.TrimSpace(linha), "#") ||
				strings.HasPrefix(strings.TrimSpace(linha), "//") {
				ref.Detail = "linha comentada"
			}
			report.References = append(report.References, ref)
		}

		if err := scanner.Err(); err != nil {
			report.Notes = append(report.Notes,
				fmt.Sprintf("%s não foi varrido até o fim: %v", f.Path, err))
		}
	}
}

func matchesAny(texto string, alvos []string) bool {
	if texto == "" {
		return false
	}
	baixo := strings.ToLower(texto)
	for _, a := range alvos {
		if a == "" {
			continue
		}
		if strings.Contains(baixo, strings.ToLower(a)) {
			return true
		}
	}
	return false
}

// SortReferences deixa a ordem estável: origem, lugar, linha.
func SortReferences(refs []Reference) {
	sort.SliceStable(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if !strings.EqualFold(a.Where, b.Where) {
			return strings.ToLower(a.Where) < strings.ToLower(b.Where)
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.What < b.What
	})
}
