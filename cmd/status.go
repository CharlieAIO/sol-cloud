package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Get validator status",
	Long:  "Show status and health details for a deployed validator.",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.OutOrStdout(), "status is scaffolded; implementation starts on Day 4")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
