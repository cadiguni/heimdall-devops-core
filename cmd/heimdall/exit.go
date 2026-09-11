package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
)

// exitError permite a um subcomando escolher o código de saída do processo sem
// que isso seja tratado como falha de execução.
//
// Um gate de pipeline precisa distinguir "a revisão reprovou o plano" de "o
// heimdall quebrou", e essas duas coisas não podem compartilhar o código 1.
type exitError struct {
	code int
	msg  string
}

func (e *exitError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return fmt.Sprintf("saída com código %d", e.code)
}

// isBrokenPipe informa se o erro é escrita em pipe já fechado, que é o que
// acontece em 'heimdall ... | head': quem lia saiu antes do fim da saída.
func isBrokenPipe(err error) bool {
	return errors.Is(err, syscall.EPIPE) || isPlatformBrokenPipe(err)
}

// exitCodeFor traduz o erro final da execução em código de saída, imprimindo a
// mensagem no stderr quando há algo a dizer ao usuário.
func exitCodeFor(err error) int {
	// Pipe fechado pelo leitor não é falha: 'heimdall ... | head' entregou o
	// que foi pedido. Sai em silêncio, como qualquer ferramenta de linha de
	// comando.
	if isBrokenPipe(err) {
		return 0
	}

	var exitErr *exitError
	if errors.As(err, &exitErr) {
		if exitErr.msg != "" {
			fmt.Fprintln(os.Stderr, "heimdall:", exitErr.msg)
		}
		return exitErr.code
	}

	if errors.Is(err, context.Canceled) {
		// Interrompido por sinal: convenção 128 + SIGINT.
		return 130
	}

	fmt.Fprintln(os.Stderr, "heimdall:", err)
	return 1
}
