package terraform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/cadiguni/heimdall-devops-core/internal/azure"
)

// supportedStateVersion é o formato de arquivo de state que este código lê.
// A versão 4 vale desde o Terraform 0.12.
const supportedStateVersion = 4

// StateResource é um recurso encontrado no state, reduzido ao que é seguro
// exibir.
//
// Um arquivo de state guarda os atributos dos recursos em texto puro — senha de
// banco, chave de acesso, connection string. Nada disso é lido para cá: o
// parser abaixo nem declara o campo "attributes", então os valores não chegam a
// existir nesta struct.
type StateResource struct {
	Address  string `json:"address"`
	Mode     string `json:"mode"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
	Tainted  bool   `json:"tainted,omitempty"`
}

// StateInspection é o resumo de um arquivo de state.
type StateInspection struct {
	Path             string `json:"path"`
	Container        string `json:"container,omitempty"`
	Version          int    `json:"version"`
	TerraformVersion string `json:"terraform_version,omitempty"`
	Serial           uint64 `json:"serial"`
	Lineage          string `json:"lineage,omitempty"`

	// Managed e Data contam instâncias, não blocos: um recurso com count = 3
	// conta 3.
	Managed int `json:"managed"`
	Data    int `json:"data"`

	// Outputs traz só os nomes. O valor de um output vai para o state em texto
	// puro e é um lugar clássico de secret vazado.
	Outputs []string `json:"outputs,omitempty"`

	Resources []StateResource `json:"resources"`
}

// Empty informa se o state não rastreia recurso nenhum.
func (s *StateInspection) Empty() bool {
	return s.Managed == 0 && s.Data == 0
}

// rawState espelha o arquivo de state v4, só com os campos que interessam.
//
// A ausência de "attributes" aqui é deliberada: campo não declarado é campo que
// o encoding/json descarta, então valor de atributo nunca entra em memória
// estruturada nem em saída.
type rawState struct {
	Version          int                        `json:"version"`
	TerraformVersion string                     `json:"terraform_version"`
	Serial           uint64                     `json:"serial"`
	Lineage          string                     `json:"lineage"`
	Outputs          map[string]json.RawMessage `json:"outputs"`
	Resources        []rawResource              `json:"resources"`
}

type rawResource struct {
	Module    string        `json:"module"`
	Mode      string        `json:"mode"`
	Type      string        `json:"type"`
	Name      string        `json:"name"`
	Provider  string        `json:"provider"`
	Instances []rawInstance `json:"instances"`
}

type rawInstance struct {
	IndexKey interface{} `json:"index_key"`
	Status   string      `json:"status"`
}

// InspectState lê um arquivo de state e resume o que ele rastreia.
func InspectState(r io.Reader, path string) (*StateInspection, error) {
	var raw rawState
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, fmt.Errorf("state inválido: %w", err)
	}

	if raw.Version == 0 {
		return nil, fmt.Errorf("não parece um arquivo de state: campo version ausente")
	}
	if raw.Version != supportedStateVersion {
		return nil, fmt.Errorf("versão de state não suportada: %d (este comando lê a %d)",
			raw.Version, supportedStateVersion)
	}

	inspection := &StateInspection{
		Path:             path,
		Version:          raw.Version,
		TerraformVersion: raw.TerraformVersion,
		Serial:           raw.Serial,
		Lineage:          raw.Lineage,
		Outputs:          outputNames(raw.Outputs),
		Resources:        []StateResource{},
	}

	for _, res := range raw.Resources {
		for _, inst := range res.Instances {
			inspection.Resources = append(inspection.Resources, StateResource{
				Address:  resourceAddress(res, inst),
				Mode:     res.Mode,
				Type:     res.Type,
				Name:     res.Name,
				Provider: providerName(res.Provider),
				Tainted:  inst.Status == "tainted",
			})

			if res.Mode == "data" {
				inspection.Data++
			} else {
				inspection.Managed++
			}
		}
	}

	sort.Slice(inspection.Resources, func(i, j int) bool {
		return inspection.Resources[i].Address < inspection.Resources[j].Address
	})

	return inspection, nil
}

// InspectStateBlob baixa um state do container e o inspeciona.
func InspectStateBlob(ctx context.Context, downloader azure.BlobDownloader, path string) (*StateInspection, error) {
	body, err := downloader.DownloadBlob(ctx, path)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	return InspectState(body, path)
}

// resourceAddress remonta o endereço do recurso como o Terraform o escreve:
// prefixo do módulo, "data." para data source, e a chave de count/for_each.
func resourceAddress(res rawResource, inst rawInstance) string {
	var b strings.Builder

	if res.Module != "" {
		b.WriteString(res.Module)
		b.WriteString(".")
	}
	if res.Mode == "data" {
		b.WriteString("data.")
	}
	b.WriteString(res.Type)
	b.WriteString(".")
	b.WriteString(res.Name)

	switch key := inst.IndexKey.(type) {
	case nil:
		// Recurso sem count nem for_each: endereço sem índice.
	case string:
		fmt.Fprintf(&b, "[%q]", key)
	case float64:
		// O JSON traz número como float64; índice de count é inteiro.
		fmt.Fprintf(&b, "[%d]", int64(key))
	default:
		fmt.Fprintf(&b, "[%v]", key)
	}

	return b.String()
}

// providerName tira o embrulho `provider["registry.terraform.io/hashicorp/azurerm"]`
// que o state usa, deixando só a fonte.
func providerName(raw string) string {
	inicio := strings.Index(raw, `["`)
	fim := strings.LastIndex(raw, `"]`)
	if inicio == -1 || fim <= inicio {
		return raw
	}
	return raw[inicio+2 : fim]
}

func outputNames(outputs map[string]json.RawMessage) []string {
	if len(outputs) == 0 {
		return nil
	}

	names := make([]string, 0, len(outputs))
	for name := range outputs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
