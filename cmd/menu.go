package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/CharlieAIO/sol-cloud/internal/ui"
	"github.com/CharlieAIO/sol-cloud/internal/utils"
	"github.com/spf13/cobra"
)

var mainMenuOptions = []utils.Option{
	{Key: "deploy", Label: "Deploy validator"},
	{Key: "status", Label: "Check status"},
	{Key: "init", Label: "Create or update hidden project config"},
	{Key: "auth-fly", Label: "Connect Fly.io"},
	{Key: "auth-railway", Label: "Connect Railway"},
	{Key: "watch", Label: "Watch validator"},
	{Key: "clone-program", Label: "Clone Solana program"},
	{Key: "destroy", Label: "Destroy validator"},
	{Key: "help", Label: "Show command help"},
	{Key: "quit", Label: "Exit"},
}

func runMainMenu(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	reader := bufio.NewReader(cmd.InOrStdin())

	for {
		ui.Header(out, "Sol-Cloud")
		ui.Fields(out,
			ui.Field{Label: "Mode", Value: "interactive"},
			ui.Field{Label: "Direct commands", Value: "sol-cloud deploy, sol-cloud status, sol-cloud init"},
		)
		fmt.Fprintln(out)

		choice, err := utils.SelectOptionArrow(cmd.InOrStdin(), out, "Command", mainMenuOptions, "deploy")
		if err != nil {
			if errors.Is(err, utils.ErrManualSelection) {
				choice, err = utils.String(reader, out, "Command", "deploy", true)
			}
			if err != nil {
				return err
			}
		}
		choice = strings.ToLower(strings.TrimSpace(choice))

		if choice == "quit" || choice == "exit" {
			fmt.Fprintln(out, "done")
			return nil
		}

		err = runMenuChoice(cmd, reader, choice)
		if err != nil {
			fmt.Fprintf(out, "\nerror: %v\n", err)
		}

		if choice == "watch" && err == nil {
			return nil
		}

		fmt.Fprintln(out)
		if ok, waitErr := waitForMenu(reader, out); waitErr != nil {
			return waitErr
		} else if !ok {
			return nil
		}
	}
}

func runMenuChoice(cmd *cobra.Command, reader *bufio.Reader, choice string) error {
	switch choice {
	case "deploy":
		return runExistingCommand(cmd, deployCmd, nil)
	case "status":
		return runExistingCommand(cmd, statusCmd, nil)
	case "init":
		return runExistingCommand(cmd, initCmd, nil)
	case "auth-fly", "fly":
		return runExistingCommand(cmd, authFlyCmd, nil)
	case "auth-railway", "railway":
		return runExistingCommand(cmd, authRailwayCmd, nil)
	case "watch":
		return runExistingCommand(cmd, watchCmd, nil)
	case "destroy":
		return runExistingCommand(cmd, destroyCmd, nil)
	case "clone-program", "clone":
		programID, err := utils.String(reader, cmd.OutOrStdout(), "Program ID", "", true)
		if err != nil {
			return err
		}
		return runExistingCommand(cmd, cloneCmd, []string{strings.TrimSpace(programID)})
	case "help":
		return cmd.Help()
	default:
		return fmt.Errorf("unknown menu command %q", choice)
	}
}

func runExistingCommand(parent, child *cobra.Command, args []string) error {
	child.SetIn(parent.InOrStdin())
	child.SetOut(parent.OutOrStdout())
	child.SetErr(parent.ErrOrStderr())
	child.SetContext(parent.Context())
	if child.RunE != nil {
		return child.RunE(child, args)
	}
	if child.Run != nil {
		child.Run(child, args)
		return nil
	}
	return fmt.Errorf("%s cannot be run directly from the menu", child.CommandPath())
}

func waitForMenu(reader *bufio.Reader, out io.Writer) (bool, error) {
	fmt.Fprint(out, "Press Enter to return to menu, or q then Enter to exit: ")
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer != "q" && answer != "quit" && answer != "exit", nil
}
