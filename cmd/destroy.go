package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
	"github.com/CharlieAIO/sol-cloud/internal/providers"
	"github.com/spf13/cobra"
)

var (
	destroyName string
	destroyYes  bool
)

var destroyCmd = &cobra.Command{
	Use:   "destroy [name]",
	Short: "Destroy a deployed validator",
	Long:  "Destroy a deployed validator and tear down cloud resources.",
	Example: `  sol-cloud destroy
  sol-cloud destroy --yes`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := strings.TrimSpace(destroyName)
		if name == "" && len(args) > 0 {
			name = strings.TrimSpace(args[0])
		}

		projectDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		state, err := appconfig.LoadState(projectDir)
		if err != nil {
			return fmt.Errorf("load local deployment state: %w", err)
		}

		if name == "" {
			record, resolveErr := state.ResolveDeployment("")
			if resolveErr != nil {
				if errors.Is(resolveErr, appconfig.ErrNoDeployments) {
					return errors.New("no deployments found; pass --name to destroy a specific app")
				}
				return resolveErr
			}
			name = record.Name
		}

		if !destroyYes {
			confirmed, confirmErr := confirmDestroy(cmd, name)
			if confirmErr != nil {
				return confirmErr
			}
			if !confirmed {
				fmt.Fprintln(cmd.OutOrStdout(), "destroy cancelled")
				return nil
			}
		}

		provider := providers.NewFlyProvider()
		if err := provider.Destroy(cmd.Context(), name); err != nil {
			return err
		}

		state.RemoveDeployment(name)
		if err := appconfig.SaveState(projectDir, state); err != nil {
			return fmt.Errorf("destroyed app but failed to update local state: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "validator destroyed: %s\n", name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(destroyCmd)

	destroyCmd.Flags().StringVar(&destroyName, "name", "", "Deployment name (defaults to last deployment from local state)")
	destroyCmd.Flags().BoolVar(&destroyYes, "yes", false, "Skip interactive confirmation")
}

func confirmDestroy(cmd *cobra.Command, name string) (bool, error) {
	fmt.Fprintf(cmd.OutOrStdout(), "this will destroy '%s'. continue? (y/N): ", name)
	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read confirmation: %w", err)
	}

	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
