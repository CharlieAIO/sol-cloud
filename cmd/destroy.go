package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy a deployed validator",
	Long:  "Destroy a deployed validator and tear down cloud resources.",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.OutOrStdout(), "destroy is scaffolded; implementation starts on Day 4")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(destroyCmd)
}
