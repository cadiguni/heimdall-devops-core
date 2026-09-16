package terraform

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
)

// loadFixture lê um plano de testdata/, gerado de verdade por
// `terraform show -json` (ver README de testdata).
func loadFixture(t *testing.T, name string) *tfjson.Plan {
	t.Helper()

	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("abrindo fixture: %v", err)
	}
	defer f.Close()

	plan, err := ParsePlanJSON(f)
	if err != nil {
		t.Fatalf("parseando fixture %s: %v", name, err)
	}
	return plan
}

func TestReviewPlanMixed(t *testing.T) {
	review := ReviewPlan(loadFixture(t, "plan_mixed.json"))

	want := Summary{Create: 1, Update: 1, Delete: 1, Replace: 1, NoOp: 1}
	if review.Summary != want {
		t.Errorf("resumo = %+v, quero %+v", review.Summary, want)
	}

	if got := review.Summary.Changed(); got != 4 {
		t.Errorf("Changed() = %d, quero 4", got)
	}

	if !review.HasDestructive() {
		t.Fatal("HasDestructive() = false, quero true")
	}

	if len(review.Destructive) != 2 {
		t.Fatalf("destrutivas = %d, quero 2: %+v", len(review.Destructive), review.Destructive)
	}

	// Ordenado por endereço.
	del := review.Destructive[0]
	if del.Address != "terraform_data.to_delete" || del.Kind != KindDelete {
		t.Errorf("primeira destrutiva = %s/%s, quero terraform_data.to_delete/delete", del.Address, del.Kind)
	}
	if del.Reason != "sem bloco de configuração correspondente" {
		t.Errorf("motivo do delete = %q", del.Reason)
	}

	rep := review.Destructive[1]
	if rep.Address != "terraform_data.to_replace" || rep.Kind != KindReplace {
		t.Errorf("segunda destrutiva = %s/%s, quero terraform_data.to_replace/replace", rep.Address, rep.Kind)
	}
	if rep.Reason != "atributo alterado não suporta update in-place" {
		t.Errorf("motivo do replace = %q", rep.Reason)
	}
	if rep.CreateBeforeDestroy {
		t.Error("CreateBeforeDestroy = true, mas o plano é delete-then-create")
	}

	if review.TerraformVersion == "" {
		t.Error("TerraformVersion vazio")
	}
	if review.Incomplete == nil || *review.Incomplete {
		t.Errorf("Incomplete = %v, quero false", review.Incomplete)
	}
}

func TestReviewPlanNoChanges(t *testing.T) {
	review := ReviewPlan(loadFixture(t, "plan_no_changes.json"))

	if review.Summary.Changed() != 0 {
		t.Errorf("Changed() = %d, quero 0 (%+v)", review.Summary.Changed(), review.Summary)
	}
	if review.HasDestructive() {
		t.Errorf("HasDestructive() = true em plano sem mudanças: %+v", review.Destructive)
	}
	// Destructive precisa serializar como [] e não null, para quem consome o JSON.
	if review.Destructive == nil {
		t.Error("Destructive = nil, quero slice vazio")
	}
}

func TestReviewPlanIncomplete(t *testing.T) {
	review := ReviewPlan(loadFixture(t, "plan_targeted_incomplete.json"))

	if review.Incomplete == nil {
		t.Fatal("Incomplete = nil, quero true (plano gerado com -target)")
	}
	if !*review.Incomplete {
		t.Error("Incomplete = false, quero true")
	}
}

// A fixture veio de um apply seguido de alteração do arquivo por fora do
// Terraform: o provider local detecta o hash diferente e reporta o recurso
// como sumido.
func TestReviewPlanDrift(t *testing.T) {
	review := ReviewPlan(loadFixture(t, "plan_com_drift.json"))

	if !review.HasDrift() {
		t.Fatal("HasDrift() = false, quero true")
	}
	if len(review.Drift) != 1 {
		t.Fatalf("drift = %d, quero 1: %+v", len(review.Drift), review.Drift)
	}

	d := review.Drift[0]
	if d.Address != "local_file.config" {
		t.Errorf("endereço = %q", d.Address)
	}
	if d.Kind != KindDelete {
		t.Errorf("Kind = %q, quero delete", d.Kind)
	}

	// Drift é separado das mudanças planejadas: aqui o plano só cria, não
	// destrói nada.
	if review.HasDestructive() {
		t.Errorf("drift não pode virar operação destrutiva: %+v", review.Destructive)
	}
	if review.Summary.Create != 1 {
		t.Errorf("resumo = %+v, quero 1 create", review.Summary)
	}
}

// Plano sem drift precisa serializar drift como [] e não null.
func TestReviewPlanSemDrift(t *testing.T) {
	review := ReviewPlan(loadFixture(t, "plan_mixed.json"))

	if review.HasDrift() {
		t.Errorf("drift inesperado: %+v", review.Drift)
	}
	if review.Drift == nil {
		t.Error("Drift = nil, quero slice vazio")
	}
}

func TestCollectDriftIgnoraNoOpEDataSource(t *testing.T) {
	plan := &tfjson.Plan{
		FormatVersion: "1.2",
		ResourceDrift: []*tfjson.ResourceChange{
			managed("terraform_data.alterado", tfjson.ActionUpdate),
			managed("terraform_data.igual", tfjson.ActionNoop),
			{
				Address: "data.azurerm_resource_group.rg",
				Mode:    tfjson.DataResourceMode,
				Change:  &tfjson.Change{Actions: tfjson.Actions{tfjson.ActionRead}},
			},
			nil,
		},
	}

	drift := ReviewPlan(plan).Drift

	if len(drift) != 1 {
		t.Fatalf("drift = %+v, quero só o recurso alterado", drift)
	}
	if drift[0].Kind != KindUpdate {
		t.Errorf("Kind = %q, quero update", drift[0].Kind)
	}
}

// As entradas de drift carregam os valores dos atributos, com os mesmos
// secrets de um plano comum.
func TestDriftNaoVazaValoresDeAtributos(t *testing.T) {
	const secret = "SENHA-NO-DRIFT"

	change := managed("terraform_data.com_secret", tfjson.ActionUpdate)
	change.Change.Before = map[string]interface{}{"password": secret}
	change.Change.After = map[string]interface{}{"password": secret + "-alterada"}

	review := ReviewPlan(&tfjson.Plan{
		FormatVersion: "1.2",
		ResourceDrift: []*tfjson.ResourceChange{change},
	})

	asJSON, err := json.Marshal(review)
	if err != nil {
		t.Fatalf("serializando: %v", err)
	}
	if bytes.Contains(asJSON, []byte(secret)) {
		t.Errorf("JSON contém valor de atributo: %s", asJSON)
	}

	var text bytes.Buffer
	if err := WriteTextReport(&text, review); err != nil {
		t.Fatalf("WriteTextReport: %v", err)
	}
	if strings.Contains(text.String(), secret) {
		t.Errorf("texto contém valor de atributo:\n%s", text.String())
	}
}

func TestIsIncomplete(t *testing.T) {
	sim, nao := true, false

	tests := []struct {
		name  string
		field *bool
		want  bool
	}{
		{"plano incompleto", &sim, true},
		{"plano completo", &nao, false},
		// Terraform < 1.8 não reporta o campo. "Não sei" não pode virar
		// "está incompleto", senão a revisão reprova tudo nessas versões.
		{"campo ausente", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			review := &PlanReview{Incomplete: tt.field}
			if got := review.IsIncomplete(); got != tt.want {
				t.Errorf("IsIncomplete() = %v, quero %v", got, tt.want)
			}
		})
	}
}

func TestReviewPlanIncompletoComDestruicao(t *testing.T) {
	review := ReviewPlan(loadFixture(t, "plan_incomplete_destructive.json"))

	if !review.IsIncomplete() {
		t.Error("IsIncomplete() = false, quero true")
	}
	if !review.HasDestructive() {
		t.Error("HasDestructive() = false, quero true")
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name    string
		actions tfjson.Actions
		want    ChangeKind
		known   bool
	}{
		{"no-op", tfjson.Actions{tfjson.ActionNoop}, KindNoOp, true},
		{"create", tfjson.Actions{tfjson.ActionCreate}, KindCreate, true},
		{"update", tfjson.Actions{tfjson.ActionUpdate}, KindUpdate, true},
		{"delete", tfjson.Actions{tfjson.ActionDelete}, KindDelete, true},
		{"read", tfjson.Actions{tfjson.ActionRead}, KindRead, true},
		{"forget", tfjson.Actions{tfjson.ActionForget}, KindForget, true},
		{"destroy-before-create", tfjson.Actions{tfjson.ActionDelete, tfjson.ActionCreate}, KindReplace, true},
		{"create-before-destroy", tfjson.Actions{tfjson.ActionCreate, tfjson.ActionDelete}, KindReplace, true},
		{"vazio", tfjson.Actions{}, "", false},
		{"combinação desconhecida", tfjson.Actions{tfjson.ActionUpdate, tfjson.ActionDelete}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, known := classify(tt.actions)
			if got != tt.want || known != tt.known {
				t.Errorf("classify(%v) = %q,%v; quero %q,%v", tt.actions, got, known, tt.want, tt.known)
			}
		})
	}
}

func TestChangeKindDestructive(t *testing.T) {
	destructive := []ChangeKind{KindDelete, KindReplace, KindForget}
	safe := []ChangeKind{KindNoOp, KindCreate, KindUpdate, KindRead}

	for _, k := range destructive {
		if !k.Destructive() {
			t.Errorf("%s.Destructive() = false, quero true", k)
		}
	}
	for _, k := range safe {
		if k.Destructive() {
			t.Errorf("%s.Destructive() = true, quero false", k)
		}
	}
}

// synthPlan monta um plano em memória para os casos que o terraform_data não
// consegue produzir (forget, data source, ações desconhecidas).
func synthPlan(changes ...*tfjson.ResourceChange) *tfjson.Plan {
	return &tfjson.Plan{
		FormatVersion:    "1.2",
		TerraformVersion: "1.14.5",
		ResourceChanges:  changes,
	}
}

func managed(address string, actions ...tfjson.Action) *tfjson.ResourceChange {
	return &tfjson.ResourceChange{
		Address: address,
		Mode:    tfjson.ManagedResourceMode,
		Type:    "terraform_data",
		Name:    address,
		Change:  &tfjson.Change{Actions: actions},
	}
}

func TestReviewPlanForgetIsDestructive(t *testing.T) {
	review := ReviewPlan(synthPlan(managed("terraform_data.esquecido", tfjson.ActionForget)))

	if review.Summary.Forget != 1 {
		t.Errorf("Forget = %d, quero 1", review.Summary.Forget)
	}
	if len(review.Destructive) != 1 || review.Destructive[0].Kind != KindForget {
		t.Fatalf("forget precisa entrar em Destructive: %+v", review.Destructive)
	}
}

func TestReviewPlanDataSourceNaoEhMudanca(t *testing.T) {
	data := &tfjson.ResourceChange{
		Address: "data.terraform_remote_state.rede",
		Mode:    tfjson.DataResourceMode,
		Type:    "terraform_remote_state",
		Name:    "rede",
		Change:  &tfjson.Change{Actions: tfjson.Actions{tfjson.ActionRead}},
	}

	review := ReviewPlan(synthPlan(data))

	if review.Summary.Changed() != 0 {
		t.Errorf("data source contou como mudança: %+v", review.Summary)
	}
	if review.Summary.DataReads != 1 {
		t.Errorf("DataReads = %d, quero 1", review.Summary.DataReads)
	}
	if review.HasDestructive() {
		t.Error("data source não pode ser destrutivo")
	}
}

// Uma combinação de ações que este código não conhece precisa cair na revisão
// em vez de sumir do relatório.
func TestReviewPlanAcaoDesconhecidaViraDestrutiva(t *testing.T) {
	review := ReviewPlan(synthPlan(
		managed("terraform_data.estranho", tfjson.ActionUpdate, tfjson.ActionDelete),
	))

	if !review.HasDestructive() {
		t.Fatalf("ação desconhecida precisa ser sinalizada: %+v", review)
	}
}

func TestReviewPlanIgnoraChangeNil(t *testing.T) {
	semChange := &tfjson.ResourceChange{
		Address: "terraform_data.sem_change",
		Mode:    tfjson.ManagedResourceMode,
	}

	review := ReviewPlan(synthPlan(semChange, nil))

	if review.Summary.Changed() != 0 || review.HasDestructive() {
		t.Errorf("entradas incompletas não deveriam gerar mudanças: %+v", review)
	}
}

func TestParsePlanJSONRejeitaEntradaInvalida(t *testing.T) {
	tests := map[string]string{
		"não é JSON":       "isso não é json",
		"JSON sem formato": `{"foo":"bar"}`,
		"formato futuro":   `{"format_version":"9.0","terraform_version":"99.0.0"}`,
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParsePlanJSON(strings.NewReader(input)); err == nil {
				t.Errorf("ParsePlanJSON(%q) não retornou erro", input)
			}
		})
	}
}

// A revisão nunca pode expor valores de atributos: um plano real carrega
// senhas, chaves e referências de Key Vault em before/after.
func TestRevisaoNaoVazaValoresDeAtributos(t *testing.T) {
	const secret = "SENHA-SUPER-SECRETA"

	change := managed("terraform_data.com_secret", tfjson.ActionDelete, tfjson.ActionCreate)
	change.Change.Before = map[string]interface{}{"password": secret}
	change.Change.After = map[string]interface{}{"password": secret}
	change.Change.BeforeSensitive = map[string]interface{}{"password": true}

	review := ReviewPlan(synthPlan(change))

	asJSON, err := json.Marshal(review)
	if err != nil {
		t.Fatalf("serializando revisão: %v", err)
	}
	if bytes.Contains(asJSON, []byte(secret)) {
		t.Errorf("saída JSON contém o valor do atributo: %s", asJSON)
	}

	var text bytes.Buffer
	if err := WriteTextReport(&text, review); err != nil {
		t.Fatalf("WriteTextReport: %v", err)
	}
	if strings.Contains(text.String(), secret) {
		t.Errorf("saída de texto contém o valor do atributo:\n%s", text.String())
	}
}
