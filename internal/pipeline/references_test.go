package pipeline

import (
	"bytes"
	"strings"
	"testing"
)

const assinaturaAntiga = "92dbecc2-c36d-4af2-887d-3681969e5850"
const assinaturaNova = "51852c54-7b67-46c5-a756-c53b4bcf0504"

func pedidoReferencias() ReferenceRequest {
	return ReferenceRequest{
		Subscription: assinaturaAntiga,
		Connections: []Connection{
			{Name: "nap-dev", Type: "azurerm", SubscriptionID: assinaturaAntiga, SubscriptionName: "MPN - MPAULA"},
			{Name: "nap-prd", Type: "azurerm", SubscriptionID: assinaturaNova},
			{Name: "docker-hub", Type: "dockerregistry"},
		},
		Groups: []VariableGroupDetail{
			{
				VariableGroupSummary: VariableGroupSummary{Name: "nap-dev", ID: 7},
				Variables: []Variable{
					{Name: "subscriptionId", Value: assinaturaAntiga},
					{Name: "serviceConnection", Value: "nap-dev"},
					{Name: "regiao", Value: "eastus"},
					{Name: "clientSecret", Secret: true},
				},
			},
			{
				VariableGroupSummary: VariableGroupSummary{Name: "comum", ID: 8},
				Variables:            []Variable{{Name: "regiao", Value: "eastus"}},
			},
		},
		Files: []RepoFile{
			{Path: "main.tf", Content: "provider \"azurerm\" {\n  subscription_id = \"" + assinaturaAntiga + "\"\n}\n"},
			{Path: "azure-pipelines.yml", Content: "variables:\n  sub: " + assinaturaNova + "\n"},
		},
	}
}

func acharRef(refs []Reference, where, what string) *Reference {
	for i := range refs {
		if refs[i].Where == where && refs[i].What == what {
			return &refs[i]
		}
	}
	return nil
}

// A conexão declara a assinatura em data.subscriptionId. Comparar o campo é
// mais confiável que procurar o GUID no nome.
func TestFindReferencesAchaConexaoPeloCampo(t *testing.T) {
	r := FindReferences(pedidoReferencias())

	conexoes := r.FromSource(SourceConnection)
	if len(conexoes) != 1 {
		t.Fatalf("conexões achadas = %+v", conexoes)
	}
	if conexoes[0].Where != "nap-dev" {
		t.Errorf("achou a conexão errada: %+v", conexoes[0])
	}
	if !strings.Contains(conexoes[0].Detail, "MPN - MPAULA") {
		t.Errorf("o detalhe perdeu o nome da assinatura: %q", conexoes[0].Detail)
	}
}

// Nome citando a assinatura sem o campo apontar para ela é achado próprio: é
// nome desatualizado, que é exatamente um problema a relatar.
func TestFindReferencesDistingueNomeDeCampo(t *testing.T) {
	req := pedidoReferencias()
	req.Connections = []Connection{
		{Name: "azure-" + assinaturaAntiga, Type: "azurerm", SubscriptionID: assinaturaNova},
	}
	req.Groups = nil
	req.Files = nil

	r := FindReferences(req)

	conexoes := r.FromSource(SourceConnection)
	if len(conexoes) != 1 || conexoes[0].What != "nome" {
		t.Fatalf("conexões = %+v", conexoes)
	}
	if !strings.Contains(conexoes[0].Detail, "não é ela") {
		t.Errorf("o detalhe não separa nome de campo: %q", conexoes[0].Detail)
	}
}

func TestFindReferencesAchaVariavelPorValorEPorNome(t *testing.T) {
	r := FindReferences(pedidoReferencias())

	grupos := r.FromSource(SourceVariableGroup)
	if len(grupos) != 1 {
		t.Fatalf("variáveis achadas = %+v", grupos)
	}

	ref := acharRef(grupos, "nap-dev", "subscriptionId")
	if ref == nil {
		t.Fatalf("não achou a variável com a assinatura no valor: %+v", grupos)
	}
	if !strings.Contains(ref.Detail, "valor") {
		t.Errorf("detalhe = %q", ref.Detail)
	}
}

func TestFindReferencesAchaPorNomeDeConexaoCom(t *testing.T) {
	req := pedidoReferencias()
	req.Names = []string{"nap-dev"}

	r := FindReferences(req)

	// A variável serviceConnection = "nap-dev" só aparece com --also.
	if acharRef(r.FromSource(SourceVariableGroup), "nap-dev", "serviceConnection") == nil {
		t.Errorf("--also não alcançou o nome da service connection: %+v", r.FromSource(SourceVariableGroup))
	}
}

// Variável secreta vem com valor null da API. Não dá para procurar dentro, e
// contar como limpa seria afirmar o que não se sabe.
func TestFindReferencesVariavelSecretaVaiParaNaoVerificavel(t *testing.T) {
	r := FindReferences(pedidoReferencias())

	if len(r.SecretBlind) != 1 {
		t.Fatalf("secretas não verificáveis = %+v", r.SecretBlind)
	}
	if r.SecretBlind[0].What != "clientSecret" {
		t.Errorf("secreta errada: %+v", r.SecretBlind[0])
	}
	// E não pode ter virado achado comum.
	for _, ref := range r.References {
		if ref.What == "clientSecret" {
			t.Error("variável secreta entrou nos achados")
		}
	}
}

func TestFindReferencesArquivoComLinhaEIdentificador(t *testing.T) {
	r := FindReferences(pedidoReferencias())

	arquivos := r.FromSource(SourceRepo)
	if len(arquivos) != 1 {
		t.Fatalf("arquivos achados = %+v", arquivos)
	}
	if arquivos[0].Where != "main.tf" || arquivos[0].Line != 2 {
		t.Errorf("lugar errado: %+v", arquivos[0])
	}
	if arquivos[0].What != "subscription_id" {
		t.Errorf("identificador = %q, queria subscription_id", arquivos[0].What)
	}
}

func TestFindReferencesMarcaLinhaComentada(t *testing.T) {
	req := ReferenceRequest{
		Subscription: assinaturaAntiga,
		Files: []RepoFile{
			{Path: "main.tf", Content: "# subscription_id = \"" + assinaturaAntiga + "\"\n"},
		},
	}

	r := FindReferences(req)
	if len(r.References) != 1 {
		t.Fatalf("achados = %+v", r.References)
	}
	if r.References[0].Detail != "linha comentada" {
		t.Errorf("detalhe = %q", r.References[0].Detail)
	}
}

// Varredura conta o que olhou. "Nenhuma referência" em cima de zero grupos não
// é resposta, e o relatório precisa poder dizer a diferença.
func TestFindReferencesContaOQueVarreu(t *testing.T) {
	r := FindReferences(pedidoReferencias())

	if r.ScannedConnections != 3 || r.ScannedGroups != 2 || r.ScannedFiles != 2 {
		t.Errorf("varridos = %d conexões, %d grupos, %d arquivos",
			r.ScannedConnections, r.ScannedGroups, r.ScannedFiles)
	}
}

func TestFindReferencesSemAchadoNaoEErro(t *testing.T) {
	req := pedidoReferencias()
	req.Subscription = "99999999-9999-9999-9999-999999999999"

	r := FindReferences(req)
	if r.Found() {
		t.Errorf("achou o que não existe: %+v", r.References)
	}
	if r.ScannedGroups == 0 {
		t.Error("perdeu a contagem")
	}
}

func TestFindReferencesIgnoraCaixaNoGUID(t *testing.T) {
	req := ReferenceRequest{
		Subscription: strings.ToUpper(assinaturaAntiga),
		Connections:  []Connection{{Name: "c", SubscriptionID: assinaturaAntiga}},
		Files: []RepoFile{
			{Path: "a.tf", Content: "id = \"" + strings.ToUpper(assinaturaAntiga) + "\""},
		},
	}

	r := FindReferences(req)
	if len(r.References) != 2 {
		t.Errorf("achados = %+v", r.References)
	}
}

// --- renderização ---

// O relatório vai para log de pipeline. Valor de variável pode ser connection
// string com a assinatura no meio, então nenhum valor pode sair.
func TestWriteReferencesNaoImprimeValor(t *testing.T) {
	req := pedidoReferencias()
	req.Groups[0].Variables = append(req.Groups[0].Variables, Variable{
		Name:  "cosmos",
		Value: "AccountEndpoint=https://x;AccountKey=SEGREDO-NAO-PODE-SAIR;sub=" + assinaturaAntiga,
	})

	var buf bytes.Buffer
	if err := WriteReferences(&buf, FindReferences(req)); err != nil {
		t.Fatalf("WriteReferences: %v", err)
	}

	if strings.Contains(buf.String(), "SEGREDO-NAO-PODE-SAIR") {
		t.Errorf("valor de variável vazou:\n%s", buf.String())
	}
	// Mas o achado precisa aparecer: não imprimir valor não é não relatar.
	if !strings.Contains(buf.String(), "cosmos") {
		t.Errorf("o achado desapareceu junto com o valor:\n%s", buf.String())
	}
}

func TestWriteReferencesMostraAsTresSecoes(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteReferences(&buf, FindReferences(pedidoReferencias())); err != nil {
		t.Fatalf("WriteReferences: %v", err)
	}
	saida := buf.String()

	for _, querido := range []string{
		assinaturaAntiga,
		"Service connections", "Variable Groups (library)", "Repositório",
		"main.tf:2",
		"Não verificáveis: variáveis secretas",
		"Nada foi alterado",
	} {
		if !strings.Contains(saida, querido) {
			t.Errorf("saída sem %q:\n%s", querido, saida)
		}
	}
}

// Varredura vazia tem que se denunciar: "nenhuma referência" sobre nada varrido
// pareceria uma resposta tranquilizadora.
func TestWriteReferencesVarreduraVaziaAvisa(t *testing.T) {
	var buf bytes.Buffer
	r := FindReferences(ReferenceRequest{Subscription: assinaturaAntiga})

	if err := WriteReferences(&buf, r); err != nil {
		t.Fatalf("WriteReferences: %v", err)
	}
	if !strings.Contains(buf.String(), "não é uma resposta") {
		t.Errorf("varredura vazia não se denunciou:\n%s", buf.String())
	}
}

func TestWriteReferencesMostraNotas(t *testing.T) {
	r := FindReferences(pedidoReferencias())
	r.Notes = []string{"1 diretório(s) .terraform foram pulados (cópia ou cache)"}

	var buf bytes.Buffer
	if err := WriteReferences(&buf, r); err != nil {
		t.Fatalf("WriteReferences: %v", err)
	}
	if !strings.Contains(buf.String(), ".terraform") {
		t.Errorf("nota não apareceu:\n%s", buf.String())
	}
}

// Variável de library apontando para uma conexão que não está na listagem do
// projeto é descasamento, e foi o que apareceu na primeira execução contra a
// organização real: nove grupos citando uma conexão ausente.
func TestFindReferencesNotaConexaoAusente(t *testing.T) {
	req := pedidoReferencias()
	// Nenhuma conexão aponta para a assinatura, mas a library cita.
	req.Connections = []Connection{
		{Name: "outra", Type: "azurerm", SubscriptionID: assinaturaNova},
	}

	r := FindReferences(req)

	if len(r.FromSource(SourceVariableGroup)) == 0 {
		t.Fatal("a premissa do teste caiu: a library precisa ter achado")
	}
	if len(r.Notes) == 0 {
		t.Fatal("sem nota sobre o descasamento")
	}
	if !strings.Contains(r.Notes[0], "nenhuma das 1 service connections") {
		t.Errorf("nota = %q", r.Notes[0])
	}
	// É nota, não veredito: não dá para saber por que a conexão não está lá.
	if !strings.Contains(r.Notes[0], "outro projeto") {
		t.Errorf("a nota conclui mais do que sabe: %q", r.Notes[0])
	}
}

// Com conexão e library batendo, não há descasamento a relatar.
func TestFindReferencesSemNotaQuandoConexaoBate(t *testing.T) {
	r := FindReferences(pedidoReferencias())

	for _, n := range r.Notes {
		if strings.Contains(n, "nenhuma das") {
			t.Errorf("nota de descasamento sem descasamento: %q", n)
		}
	}
}
