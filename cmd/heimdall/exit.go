package main

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// exitCodeFor traduz o erro final da execução em código de saída, imprimindo a
// mensagem no stderr quando há algo a dizer ao usuário.
func exitCodeFor(err error) int {
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
