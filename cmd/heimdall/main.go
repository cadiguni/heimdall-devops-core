// Command heimdall é a CLI interna de automação de DevOps.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		// Cobra já imprime o erro de uso; aqui só garantimos exit code != 0.
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "heimdall:", err)
		}
		os.Exit(1)
	}
}
