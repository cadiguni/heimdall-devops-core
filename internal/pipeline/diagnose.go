package pipeline

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Severity separa o que quebrou a pipeline do que ainda não quebrou mas vai.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Finding é uma ocorrência reconhecida no log.
type Finding struct {
	// Signature é o identificador estável da assinatura, para agrupar e para
	// quem consumir o JSON.
	Signature string   `json:"signature"`
	Title     string   `json:"title"`
	Severity  Severity `json:"severity"`

	// Cause é o que costuma estar por trás; Action é o que fazer.
	Cause  string `json:"cause"`
	Action string `json:"action"`

	// Line é a linha do log onde a assinatura casou (1-indexado), e Evidence é
	// essa linha já limpa dos enfeites de moldura do Terraform.
	Line     int    `json:"line"`
	Evidence string `json:"evidence"`

	// Details traz o que foi extraído da mensagem: nome da variável, ID do
	// recurso, endereço no state.
	Details map[string]string `json:"details,omitempty"`

	// Commands são comandos prontos para resolver, quando o log traz dado
	// suficiente para montá-los com segurança.
	Commands []string `json:"commands,omitempty"`
}

// Diagnosis é o resultado da varredura de um log.
type Diagnosis struct {
	Source       string    `json:"source,omitempty"`
	LinesScanned int       `json:"lines_scanned"`
	Findings     []Finding `json:"findings"`
}

// HasFindings informa se algo foi reconhecido.
func (d *Diagnosis) HasFindings() bool {
	return len(d.Findings) > 0
}

// Errors devolve só os achados que quebraram a execução.
func (d *Diagnosis) Errors() []Finding {
	var errs []Finding
	for _, f := range d.Findings {
		if f.Severity == SeverityError {
			errs = append(errs, f)
		}
	}
	return errs
}

// maxLineLen limita a linha lida. Logs de pipeline trazem mensagens de erro
// longas em uma linha só (a do CDN passa de 700 bytes), mas nada perto disso.
const maxLineLen = 1024 * 1024

// Diagnose varre um log de pipeline e reconhece as falhas conhecidas.
//
// Cada linha é testada contra o catálogo na ordem, e a primeira assinatura que
// casa vence — as assinaturas específicas vêm antes das genéricas.
func Diagnose(r io.Reader, source string) (*Diagnosis, error) {
	lines, err := readLines(r)
	if err != nil {
		return nil, err
	}

	diagnosis := &Diagnosis{
		Source:       source,
		LinesScanned: len(lines),
		Findings:     []Finding{},
	}

	for idx, line := range lines {
		for _, sig := range signatures {
			match := sig.re.FindStringSubmatch(line)
			if match == nil {
				continue
			}

			finding := Finding{
				Signature: sig.id,
				Title:     sig.title,
				Severity:  sig.severity,
				Cause:     sig.cause,
				Action:    sig.action,
				Line:      idx + 1,
				Evidence:  cleanLine(line),
			}
			if sig.enrich != nil {
				finding.Details, finding.Commands = sig.enrich(match, lines, idx)
			}

			diagnosis.Findings = append(diagnosis.Findings, finding)
			break
		}
	}

	return diagnosis, nil
}

func readLines(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineLen)

	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("lendo log: %w", err)
	}
	return lines, nil
}

// cleanLine tira a moldura que o Terraform desenha em volta dos erros ("╷│╵")
// e o espaço em volta, para a evidência caber no relatório.
func cleanLine(line string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "╷│╵ \t"))
}

// addressNear procura o endereço do recurso no bloco de contexto que o
// Terraform imprime logo abaixo do erro ("  with azurerm_x.y,").
//
// Devolve string vazia quando não encontra: é melhor um achado sem endereço do
// que um endereço adivinhado, porque ele vira comando de import.
func addressNear(lines []string, idx int) string {
	const lookahead = 10

	for i := idx + 1; i < len(lines) && i <= idx+lookahead; i++ {
		if match := withAddressRe.FindStringSubmatch(lines[i]); match != nil {
			return match[1]
		}
	}
	return ""
}

// firstNonEmpty devolve o primeiro grupo de captura preenchido, para
// assinaturas que usam alternação.
func firstNonEmpty(match []string) string {
	for _, group := range match[1:] {
		if group != "" {
			return group
		}
	}
	return ""
}
