package terraform

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ModuleFile é um arquivo de configuração do módulo, já lido.
//
// O domínio recebe o conteúdo e não o caminho: achar e abrir arquivo é
// responsabilidade de quem faz I/O.
type ModuleFile struct {
	Name    string
	Content string
}

// ModuleSnapshot é o que se consegue saber do diretório de trabalho sem
// executar nada que escreva.
//
// Os campos de erro são string e não error de propósito: isso é um retrato
// para o domínio decidir em cima, não um erro para propagar. Um diretório que
// não dá para inspecionar é motivo de reprovar o preflight, não de o comando
// quebrar.
type ModuleSnapshot struct {
	Dir string

	// Files são os arquivos .tf da raiz do módulo.
	Files []ModuleFile

	// JSONFileCount conta arquivos .tf.json, que este código não interpreta.
	// A contagem existe para o preflight saber que a varredura é incompleta e
	// avisar em vez de reprovar.
	JSONFileCount int

	FilesErr string

	// RawState é o state que o diretório usa de verdade, vindo de
	// 'terraform state pull'.
	RawState     string
	StatePullErr string
}

// StatePuller lê o state que um diretório de trabalho usa.
//
// É interface separada do Runner porque só lê: o preflight precisa disto antes
// de qualquer escrita, e nenhum comando que só escreve precisa disto.
type StatePuller interface {
	StatePull(ctx context.Context) (string, error)
}

// resourceBlock casa a abertura de um bloco de recurso na configuração.
var resourceBlock = regexp.MustCompile(`(?m)^[ \t]*resource[ \t]+"([^"]+)"[ \t]+"([^"]+)"`)

// lineComment casa comentário de linha inteira, em # e em //.
var lineComment = regexp.MustCompile(`(?m)^[ \t]*(#|//).*$`)

// declaredResources devolve os recursos declarados na raiz do módulo, no
// formato "tipo.nome".
func declaredResources(files []ModuleFile) map[string]bool {
	declarados := map[string]bool{}

	for _, f := range files {
		conteudo := lineComment.ReplaceAllString(f.Content, "")
		for _, m := range resourceBlock.FindAllStringSubmatch(conteudo, -1) {
			declarados[m[1]+"."+m[2]] = true
		}
	}

	return declarados
}

// configAddress reduz um endereço de state ao "tipo.nome" que apareceria num
// bloco de configuração, e diz se o recurso está dentro de um módulo.
//
// Endereço dentro de módulo não é verificável a partir da raiz: o bloco está no
// diretório do módulo, que pode vir de registry ou de git. Nesse caso o
// preflight avisa em vez de afirmar.
func configAddress(address string) (string, bool) {
	emModulo := strings.HasPrefix(address, "module.")

	// O índice de count/for_each não aparece na configuração.
	if i := strings.Index(address, "["); i >= 0 {
		address = address[:i]
	}

	// Sobra "tipo.nome" depois do último "module.x.".
	if emModulo {
		partes := strings.Split(address, ".")
		if len(partes) >= 2 {
			address = strings.Join(partes[len(partes)-2:], ".")
		}
	}

	return address, emModulo
}

// filesMentioning lista os arquivos que citam o texto dado, ignorando
// comentários de linha.
func filesMentioning(files []ModuleFile, texto string) []string {
	var achados []string

	for _, f := range files {
		conteudo := lineComment.ReplaceAllString(f.Content, "")
		if containsFold(conteudo, texto) {
			achados = append(achados, f.Name)
		}
	}

	sort.Strings(achados)
	return achados
}

// checkModule acrescenta ao plano as três verificações do diretório de
// trabalho. Todas existem para evitar o mesmo acidente.
//
// O remap faz 'state rm' e depois 'import'. O rm funciona com o recurso fora da
// configuração; o import recusa. Sem estas verificações, apontar --chdir para o
// diretório errado tira o recurso do state e falha no import — um buraco por
// uma causa que era verificável antes de escrever nada.
func checkModule(plan *RemapPlan, snap *ModuleSnapshot, remoteLineage, from string) {
	if snap != nil {
		plan.ModuleDir = snap.Dir
	}

	if snap == nil {
		plan.add("módulo inspecionado", CheckWarn,
			"--chdir não informado: a configuração não foi verificada. "+
				"O --apply exige o diretório do módulo")
		return
	}

	checkModuleState(plan, snap, remoteLineage)
	checkModuleConfig(plan, snap)
	checkModuleSubscription(plan, snap, from)
}

// checkModuleState confirma que o diretório usa este state, e não outro.
//
// Compara lineage, que é a identidade do state: dois states com os mesmos
// recursos têm lineages diferentes, e um diretório inicializado contra outro
// backend se denuncia aqui.
func checkModuleState(plan *RemapPlan, snap *ModuleSnapshot, remoteLineage string) {
	if snap.StatePullErr != "" {
		plan.add("módulo usa este state", CheckFail,
			"não deu para ler o state de %s: %s. O diretório precisa estar inicializado "+
				"contra este backend — o heimdall não roda 'terraform init'",
			snap.Dir, snap.StatePullErr)
		return
	}

	// Em diretório sem backend inicializado o 'terraform state pull' sai com
	// código 0 e não imprime nada — verificado. Sem este caso separado, o vazio
	// chegaria ao parser e viraria "state inválido: EOF", que esconde a causa.
	if strings.TrimSpace(snap.RawState) == "" {
		plan.add("módulo usa este state", CheckFail,
			"não há state em %s: o 'terraform state pull' voltou vazio, o que acontece em "+
				"diretório sem backend inicializado. Rode 'terraform init' lá — o heimdall não roda",
			snap.Dir)
		return
	}

	local, err := InspectState(strings.NewReader(snap.RawState), snap.Dir)
	if err != nil {
		plan.add("módulo usa este state", CheckFail,
			"o state de %s não foi entendido: %v", snap.Dir, err)
		return
	}

	switch {
	case local.Lineage == "" || remoteLineage == "":
		plan.add("módulo usa este state", CheckWarn,
			"sem lineage para comparar; confirme à mão que %s está inicializado contra este state",
			snap.Dir)
	case local.Lineage != remoteLineage:
		plan.add("módulo usa este state", CheckFail,
			"%s está inicializado contra outro state (lineage %s, e este é %s)",
			snap.Dir, local.Lineage, remoteLineage)
	default:
		plan.add("módulo usa este state", CheckOK, "lineage %s confere", remoteLineage)
	}
}

// checkModuleConfig confirma que cada alvo tem bloco na configuração. É a
// verificação que impede o buraco: o import recusa recurso que não está lá.
func checkModuleConfig(plan *RemapPlan, snap *ModuleSnapshot) {
	alvos := plan.Targets()
	if len(alvos) == 0 {
		return
	}

	if snap.FilesErr != "" {
		plan.add("recursos na configuração", CheckFail,
			"não deu para ler a configuração de %s: %s", snap.Dir, snap.FilesErr)
		return
	}
	if len(snap.Files) == 0 && snap.JSONFileCount == 0 {
		plan.add("recursos na configuração", CheckFail,
			"nenhum arquivo .tf em %s — este não é o diretório do módulo", snap.Dir)
		return
	}

	declarados := declaredResources(snap.Files)

	var faltando, emModulo []string
	for _, a := range alvos {
		nome, dentroDeModulo := configAddress(a.Address)
		if declarados[nome] {
			continue
		}
		if dentroDeModulo {
			emModulo = append(emModulo, a.Address)
			continue
		}
		faltando = append(faltando, a.Address)
	}

	switch {
	case len(faltando) > 0 && snap.JSONFileCount > 0:
		// A varredura é incompleta: há configuração em .tf.json, que este
		// código não interpreta. Reprovar aqui seria afirmar o que não se sabe.
		plan.add("recursos na configuração", CheckWarn,
			"%s não apareceu nos .tf, e há %d arquivo(s) .tf.json que não são interpretados aqui — confira à mão",
			strings.Join(faltando, ", "), snap.JSONFileCount)
	case len(faltando) > 0:
		plan.add("recursos na configuração", CheckFail,
			"sem bloco de configuração para %s. O 'terraform import' recusa recurso que não está "+
				"na configuração, e o 'state rm' antes dele teria tirado do state",
			strings.Join(faltando, ", "))
	case len(emModulo) > 0:
		plan.add("recursos na configuração", CheckWarn,
			"%s está dentro de módulo; o bloco mora no diretório do módulo e não dá para conferir daqui",
			strings.Join(emModulo, ", "))
	default:
		plan.add("recursos na configuração", CheckOK, "%d de %d declarados", len(alvos), len(alvos))
	}
}

// checkModuleSubscription avisa quando a configuração ainda cita a assinatura
// de origem. Remapear o state e deixar a configuração velha é trocar o problema
// de lugar: o próximo plan volta a apontar para a assinatura antiga.
//
// É aviso e não reprovação porque a citação pode ser legítima — um recurso de
// outra assinatura, uma referência cruzada — e porque a assinatura costuma vir
// de variável ou de Variable Group, onde o heimdall não enxerga.
func checkModuleSubscription(plan *RemapPlan, snap *ModuleSnapshot, from string) {
	if snap.FilesErr != "" || len(snap.Files) == 0 {
		return
	}

	citam := filesMentioning(snap.Files, from)
	if len(citam) == 0 {
		plan.add("configuração sem a assinatura antiga", CheckOK,
			"nenhum .tf cita %s", from)
		return
	}

	plan.add("configuração sem a assinatura antiga", CheckWarn,
		"%s ainda cita %s. Se for de onde sai a assinatura do provider, o próximo plan "+
			"volta para a antiga — acerte a configuração também",
		strings.Join(citam, ", "), from)
}

// ErrModuleRequired é o que o comando devolve quando pedem --apply sem o
// diretório do módulo.
func ErrModuleRequired() error {
	return fmt.Errorf("--apply exige --chdir com o diretório do módulo Terraform, " +
		"já inicializado contra este backend: sem ele não dá para verificar que o " +
		"'import' vai encontrar a configuração dos recursos")
}
