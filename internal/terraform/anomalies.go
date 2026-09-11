package terraform

import (
	"fmt"
	"path"
	"strings"
)

// emptyStateBytes é o limite abaixo do qual um state é suspeito de não
// rastrear recurso nenhum.
//
// Medido, não chutado: um state v4 recém-criado sem recursos tem 181 bytes
// (version, terraform_version, serial, lineage, outputs vazio, resources
// vazio). Um state com dois recursos simples já passa de 1500. O limite de 300
// é conservador de propósito — a heurística serve para levantar suspeita, e
// quem confirma é o 'states show'.
const emptyStateBytes = 300

// AnomalyKind identifica o tipo de irregularidade encontrada no nome ou no
// tamanho de um state.
type AnomalyKind string

const (
	// AnomalyUnexpandedVariable: o nome do blob tem uma variável que não foi
	// substituída, ex.: "app$(AliasAssinatura).tfstate". Significa um
	// 'terraform init' que criou state órfão em vez de usar o certo.
	AnomalyUnexpandedVariable AnomalyKind = "unexpanded-variable"

	// AnomalyBackendKeyPrefix: o caminho começa com "key=", ou seja o próprio
	// "key=" do -backend-config virou parte da chave do blob.
	AnomalyBackendKeyPrefix AnomalyKind = "backend-key-prefix"

	// AnomalyDanglingSeparator: o nome termina em separador antes da extensão,
	// ex.: "observability-.tfstate". Cara de interpolação que resolveu vazio.
	AnomalyDanglingSeparator AnomalyKind = "dangling-separator"

	// AnomalyPossiblyEmpty: tamanho compatível com state sem recursos.
	AnomalyPossiblyEmpty AnomalyKind = "possibly-empty"
)

// Anomaly é uma irregularidade detectada em um state do inventário.
type Anomaly struct {
	Kind   AnomalyKind `json:"kind"`
	Detail string      `json:"detail"`
}

// detectAnomalies aplica as regras a um state já montado.
//
// Todas as regras olham só nome e tamanho, que a listagem já traz: detectar
// não custa nenhuma leitura a mais no Azure.
func detectAnomalies(entry StateEntry) []Anomaly {
	var anomalies []Anomaly

	if marker := unexpandedMarker(entry.Path); marker != "" {
		anomalies = append(anomalies, Anomaly{
			Kind:   AnomalyUnexpandedVariable,
			Detail: fmt.Sprintf("o nome contém %s, variável que não foi substituída", marker),
		})
	}

	if strings.HasPrefix(entry.Path, "key=") {
		anomalies = append(anomalies, Anomaly{
			Kind:   AnomalyBackendKeyPrefix,
			Detail: `o "key=" do -backend-config virou parte do caminho do blob`,
		})
	}

	if sep := danglingSeparator(entry.Path); sep != "" {
		anomalies = append(anomalies, Anomaly{
			Kind:   AnomalyDanglingSeparator,
			Detail: fmt.Sprintf("o nome termina em %q antes da extensão, como se uma interpolação tivesse resolvido vazio", sep),
		})
	}

	if entry.SizeBytes > 0 && entry.SizeBytes < emptyStateBytes {
		anomalies = append(anomalies, Anomaly{
			Kind: AnomalyPossiblyEmpty,
			Detail: fmt.Sprintf("%d bytes, compatível com state sem recursos (confirme com 'states show')",
				entry.SizeBytes),
		})
	}

	return anomalies
}

// unexpandedMarker devolve o marcador de variável não substituída encontrado no
// nome, ou string vazia.
func unexpandedMarker(name string) string {
	// "$(...)" é sintaxe de variável do Azure DevOps; "${...}" é de shell e de
	// interpolação do Terraform. Nenhum dos dois deveria sobreviver até virar
	// nome de blob.
	for _, marker := range []string{"$(", "${"} {
		if strings.Contains(name, marker) {
			return marker + "…)"
		}
	}
	return ""
}

// danglingSeparator detecta nome terminando em separador logo antes de
// ".tfstate", ex.: "elk-.tfstate".
func danglingSeparator(name string) string {
	base := path.Base(name)

	ext := strings.LastIndex(strings.ToLower(base), stateSuffix)
	if ext <= 0 {
		return ""
	}

	stem := base[:ext]
	if stem == "" {
		return ""
	}

	switch last := stem[len(stem)-1]; last {
	case '-', '_', '.':
		return string(last)
	default:
		return ""
	}
}

// Anomalous devolve só os states com alguma irregularidade.
func (i *StateInventory) Anomalous() []StateEntry {
	var found []StateEntry
	for _, s := range i.States {
		if len(s.Anomalies) > 0 {
			found = append(found, s)
		}
	}
	return found
}
