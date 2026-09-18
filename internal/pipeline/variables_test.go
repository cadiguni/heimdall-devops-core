package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

// fakeGroupLister devolve grupos fixos, para o domínio ser exercitado sem
// chamar o Azure DevOps.
type fakeGroupLister struct {
	groups []azure.VariableGroup
	err    error
}

func (f *fakeGroupLister) ListVariableGroups(context.Context) ([]azure.VariableGroup, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.groups, nil
}

func texto(s string) *string { return &s }

func identidade(nome string) azure.IdentityRef {
	return azure.IdentityRef{DisplayName: nome}
}

func gruposDeExemplo() []azure.VariableGroup {
	return []azure.VariableGroup{
		{
			ID:   7,
			Name: "cofre-prod",
			Type: "AzureKeyVault",
			Variables: map[string]azure.VariableValue{
				"senha-banco": {IsSecret: true},
			},
			ModifiedOn: time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC),
			ModifiedBy: identidade("Beltrana"),
		},
		{
			ID:          2,
			Name:        "terraform-dev",
			Type:        "Vsts",
			Description: "backend e credenciais de dev",
			Variables: map[string]azure.VariableValue{
				"AMBIENTE":          {Value: texto("dev")},
				"ARM_CLIENT_SECRET": {IsSecret: true},
				"backendKey":        {Value: texto("dev/app.tfstate"), IsReadOnly: true},
			},
			ModifiedOn: time.Date(2026, 9, 10, 11, 0, 0, 0, time.UTC),
			ModifiedBy: identidade("Fulano de Tal"),
		},
	}
}

func listar(t *testing.T, lister azure.VariableGroupLister) *VariableGroupInventory {
	t.Helper()

	inv, err := ListVariableGroups(context.Background(), lister)
	if err != nil {
		t.Fatalf("ListVariableGroups: %v", err)
	}
	return inv
}

func TestListVariableGroupsResumeEOrdena(t *testing.T) {
	inv := listar(t, &fakeGroupLister{groups: gruposDeExemplo()})

	if len(inv.Groups) != 2 {
		t.Fatalf("grupos = %d, quero 2", len(inv.Groups))
	}
	// Ordenado por nome, sem diferenciar maiúsculas.
	if inv.Groups[0].Name != "cofre-prod" || inv.Groups[1].Name != "terraform-dev" {
		t.Errorf("ordem = %q, %q", inv.Groups[0].Name, inv.Groups[1].Name)
	}

	dev := inv.Groups[1]
	if dev.Total != 3 {
		t.Errorf("Total = %d, quero 3", dev.Total)
	}
	if dev.Secrets != 1 {
		t.Errorf("Secrets = %d, quero 1", dev.Secrets)
	}
	if dev.ModifiedBy != "Fulano de Tal" {
		t.Errorf("ModifiedBy = %q", dev.ModifiedBy)
	}
}

func TestListVariableGroupsVazio(t *testing.T) {
	inv := listar(t, &fakeGroupLister{})

	if len(inv.Groups) != 0 {
		t.Errorf("grupos = %+v, quero vazio", inv.Groups)
	}
	// Precisa serializar como [] e não null.
	if inv.Groups == nil {
		t.Error("Groups = nil, quero slice vazio")
	}
}

func TestListVariableGroupsPropagaErro(t *testing.T) {
	falha := errors.New("203 sem escopo")

	_, err := ListVariableGroups(context.Background(), &fakeGroupLister{err: falha})
	if !errors.Is(err, falha) {
		t.Errorf("erro = %v, quero %v", err, falha)
	}
}

func encontrar(t *testing.T, nameOrID string, reveal bool) *VariableGroupDetail {
	t.Helper()

	d, err := FindVariableGroup(context.Background(), &fakeGroupLister{groups: gruposDeExemplo()}, nameOrID, reveal)
	if err != nil {
		t.Fatalf("FindVariableGroup(%q): %v", nameOrID, err)
	}
	return d
}

func TestFindVariableGroupPorNomeEPorID(t *testing.T) {
	porNome := encontrar(t, "terraform-dev", false)
	porID := encontrar(t, "2", false)

	if porNome.ID != 2 || porID.Name != "terraform-dev" {
		t.Errorf("nome=%+v id=%+v", porNome.VariableGroupSummary, porID.VariableGroupSummary)
	}

	// Nome não diferencia maiúsculas: o portal aceita as duas grafias.
	if maiusculo := encontrar(t, "TERRAFORM-DEV", false); maiusculo.ID != 2 {
		t.Errorf("busca por nome deveria ignorar caixa: %+v", maiusculo.VariableGroupSummary)
	}
}

func TestFindVariableGroupInexistente(t *testing.T) {
	_, err := FindVariableGroup(context.Background(), &fakeGroupLister{groups: gruposDeExemplo()}, "nao-existe", false)
	if err == nil {
		t.Fatal("erro = nil, quero falha")
	}
	if !strings.Contains(err.Error(), "nao-existe") {
		t.Errorf("erro não cita o que foi procurado: %v", err)
	}

	if _, err := FindVariableGroup(context.Background(), &fakeGroupLister{}, "", false); err == nil {
		t.Error("nome vazio deveria ser recusado")
	}
}

// O comportamento central: sem pedido explícito, nenhum valor sai.
func TestFindVariableGroupNaoRevelaValorPorPadrao(t *testing.T) {
	d := encontrar(t, "terraform-dev", false)

	if d.ValuesRevealed {
		t.Error("ValuesRevealed = true sem pedido")
	}
	for _, v := range d.Variables {
		if v.Value != "" {
			t.Errorf("%s: valor vazou sem --show-values: %q", v.Name, v.Value)
		}
	}

	asJSON, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("serializando: %v", err)
	}
	if bytes.Contains(asJSON, []byte("dev/app.tfstate")) {
		t.Errorf("JSON contém valor de variável: %s", asJSON)
	}
}

func TestFindVariableGroupRevelaSoAsNaoSecretas(t *testing.T) {
	d := encontrar(t, "terraform-dev", true)

	if !d.ValuesRevealed {
		t.Error("ValuesRevealed = false com o pedido")
	}

	porNome := map[string]Variable{}
	for _, v := range d.Variables {
		porNome[v.Name] = v
	}

	if got := porNome["AMBIENTE"].Value; got != "dev" {
		t.Errorf("AMBIENTE = %q, quero dev", got)
	}
	if got := porNome["backendKey"]; got.Value != "dev/app.tfstate" || !got.ReadOnly {
		t.Errorf("backendKey = %+v", got)
	}
	// Secreta continua sem valor mesmo com a revelação ligada.
	if got := porNome["ARM_CLIENT_SECRET"]; got.Value != "" || !got.Secret {
		t.Errorf("ARM_CLIENT_SECRET = %+v, não pode ter valor", got)
	}
}

// Se a API mudasse e devolvesse valor numa variável secreta, o domínio ainda
// não pode repassar.
func TestValorDeSecretaNuncaEhCopiado(t *testing.T) {
	lister := &fakeGroupLister{groups: []azure.VariableGroup{{
		ID:   1,
		Name: "grupo",
		Variables: map[string]azure.VariableValue{
			"SENHA": {Value: texto("nao-deveria-vir"), IsSecret: true},
		},
	}}}

	d, err := FindVariableGroup(context.Background(), lister, "grupo", true)
	if err != nil {
		t.Fatalf("FindVariableGroup: %v", err)
	}

	if d.Variables[0].Value != "" {
		t.Errorf("valor de secreta foi copiado: %q", d.Variables[0].Value)
	}
}

func TestFindVariableGroupOrdenaVariaveis(t *testing.T) {
	d := encontrar(t, "terraform-dev", false)

	want := []string{"AMBIENTE", "ARM_CLIENT_SECRET", "backendKey"}
	for i, name := range want {
		if d.Variables[i].Name != name {
			t.Errorf("posição %d = %q, quero %q", i, d.Variables[i].Name, name)
		}
	}
}
