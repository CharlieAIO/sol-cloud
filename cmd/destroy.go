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
	"github.com/CharlieAIO/sol-cloud/internal/ui"
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

		var record appconfig.DeploymentRecord
		if name == "" {
			var resolveErr error
			record, resolveErr = state.ResolveDeployment("")
			if resolveErr != nil {
				if errors.Is(resolveErr, appconfig.ErrNoDeployments) {
					return errors.New("no deployments found; pass --name to destroy a specific app")
				}
				return resolveErr
			}
			name = record.Name
		} else {
			var resolveErr error
			record, resolveErr = state.ResolveDeployment(name)
			if resolveErr != nil && !errors.Is(resolveErr, appconfig.ErrDeploymentNotFound) {
				return resolveErr
			}
		}

		providerName := strings.TrimSpace(record.Provider)
		if providerName == "" {
			providerName = "fly"
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

		provider, err := providers.NewProvider(providerName)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		progress := ui.NewProgress(out, 3)
		progress.Start("Destroying cloud resources")
		if err := provider.Destroy(cmd.Context(), name); err != nil {
			progress.Fail("Destroy failed")
			return err
		}

		progress.Step("Updating local state")
		state.RemoveDeployment(name)
		if err := appconfig.SaveState(projectDir, state); err != nil {
			progress.Fail("Destroy failed")
			return fmt.Errorf("destroyed app but failed to update local state: %w", err)
		}

		progress.Success("Validator destroyed")
		ui.Header(out, "Destroyed")
		ui.Fields(out,
			ui.Field{Label: "Validator", Value: name},
			ui.Field{Label: "Provider", Value: providerName},
			ui.Field{Label: "State", Value: appconfig.StateFilePath(projectDir)},
		)
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
