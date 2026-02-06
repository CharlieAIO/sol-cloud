package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy a validator",
	Long:  "Deploy a Solana validator to a configured cloud provider.",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(cmd.OutOrStdout(), "deploy is scaffolded; implementation starts on Day 2/3")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)
}
