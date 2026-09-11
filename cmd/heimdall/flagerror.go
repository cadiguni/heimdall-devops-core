package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// flagErrorFunc troca a mensagem quando a flag desconhecida é, na verdade,
// sintoma de subcomando errado.
//
// O cobra faz o parse das flags antes de validar os argumentos, então
// 'heimdall terraform states inexistente --account x' reclama de "--account"
// e não do "inexistente", que é a causa. Isso esconde justamente o caso em que
// a pessoa está com um binário antigo, sem o subcomando que espera.
func flagErrorFunc(cmd *cobra.Command, err error) error {
	if !cmd.HasSubCommands() {
		return err
	}

	// Argumentos posicionais que o pflag conseguiu separar antes de falhar. Em
	// um comando-grupo o primeiro deles só pode ser um subcomando, e se
	// chegamos aqui é porque ele não existe.
	args := cmd.Flags().Args()
	if len(args) == 0 {
		return err
	}

	return fmt.Errorf("subcomando desconhecido %q para %q\nsubcomandos disponíveis: %s",
		args[0], cmd.CommandPath(), strings.Join(subcommandNames(cmd), ", "))
}

func subcommandNames(cmd *cobra.Command) []string {
	var names []string
	for _, sub := range cmd.Commands() {
		if sub.IsAvailableCommand() {
			names = append(names, sub.Name())
		}
	}
	sort.Strings(names)
	return names
}
