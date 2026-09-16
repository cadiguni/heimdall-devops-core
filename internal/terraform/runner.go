package terraform

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/hashicorp/terraform-exec/tfexec"
)

// Runner executa as operações que mexem no state de fato. A interface existe
// para o comando ser testável sem invocar o terraform.
type Runner interface {
	Import(ctx context.Context, address, resourceID string) error
	StateRm(ctx context.Context, address string) error
	StateMv(ctx context.Context, source, destination string) error
}

// ExecImporter roda o terraform de verdade, no diretório de trabalho dado.
//
// Só executa operações de state: nunca init, nunca apply.
type ExecImporter struct {
	tf *tfexec.Terraform
}

// NewExecImporter localiza o binário do terraform e prepara a execução em
// workingDir.
//
// Não roda 'init': o diretório precisa já estar inicializado contra o backend
// certo. Rodar init aqui poderia reconfigurar o backend de um diretório de
// trabalho alheio, o que num comando que se propõe seguro seria contraditório.
func NewExecImporter(workingDir string, stdout, stderr io.Writer) (*ExecImporter, error) {
	execPath, err := exec.LookPath("terraform")
	if err != nil {
		return nil, fmt.Errorf("terraform não encontrado no PATH: %w", err)
	}

	tf, err := tfexec.NewTerraform(workingDir, execPath)
	if err != nil {
		return nil, fmt.Errorf("preparando terraform em %s: %w", workingDir, err)
	}

	tf.SetStdout(stdout)
	tf.SetStderr(stderr)

	return &ExecImporter{tf: tf}, nil
}

// Import executa 'terraform import'.
func (e *ExecImporter) Import(ctx context.Context, address, resourceID string) error {
	if err := e.tf.Import(ctx, address, resourceID); err != nil {
		return fmt.Errorf("terraform import falhou: %w", err)
	}
	return nil
}

// StateRm tira o recurso do state. A infraestrutura no Azure continua
// existindo — só deixa de ser rastreada.
func (e *ExecImporter) StateRm(ctx context.Context, address string) error {
	if err := e.tf.StateRm(ctx, address); err != nil {
		return fmt.Errorf("terraform state rm falhou: %w", err)
	}
	return nil
}

// StateMv troca o endereço de um recurso dentro do mesmo state.
func (e *ExecImporter) StateMv(ctx context.Context, source, destination string) error {
	if err := e.tf.StateMv(ctx, source, destination); err != nil {
		return fmt.Errorf("terraform state mv falhou: %w", err)
	}
	return nil
}

// verificação em tempo de compilação de que o executor real satisfaz a
// interface usada pelos comandos.
var _ Runner = (*ExecImporter)(nil)
