package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a starter config file",
	Long:  "Create a .sol-cloud.yml starter file in the current directory.",
	RunE: func(cmd *cobra.Command, args []string) error {
		const file = ".sol-cloud.yml"
		if _, err := os.Stat(file); err == nil {
			fmt.Fprintln(cmd.OutOrStdout(), ".sol-cloud.yml already exists")
			return nil
		}

		starter := `provider: fly
app_name: ""
region: ord
validator:
  slots_per_epoch: 432000
  clock_multiplier: 1
  compute_unit_limit: 200000
`

		if err := os.WriteFile(file, []byte(starter), 0o644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}

		fmt.Fprintln(cmd.OutOrStdout(), "created .sol-cloud.yml")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}
