package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
	"github.com/CharlieAIO/sol-cloud/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string
var version = "dev"

var rootCmd = &cobra.Command{
	Use:          "sol-cloud",
	Short:        "Deploy Solana validators to Fly.io or Railway",
	Long:         "sol-cloud deploys and manages Solana validator environments on Fly.io or Railway.",
	Version:      version,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !ui.IsTerminal(cmd.InOrStdin()) || !ui.IsTerminal(cmd.OutOrStdout()) {
			return cmd.Help()
		}
		return runMainMenu(cmd)
	},
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is hidden per-project config)")
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: unable to resolve project config path: %v\n", err)
			return
		}
		hiddenPath, err := appconfig.ProjectConfigPath(wd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: unable to resolve project config path: %v\n", err)
			return
		}
		if _, err := os.Stat(hiddenPath); err == nil {
			viper.SetConfigFile(hiddenPath)
		} else {
			legacyPath := appconfig.LegacyProjectConfigPath(wd)
			if _, legacyErr := os.Stat(legacyPath); legacyErr == nil {
				viper.SetConfigFile(legacyPath)
			} else {
				viper.SetConfigFile(hiddenPath)
			}
		}
	}

	viper.SetEnvPrefix("SOL_CLOUD")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			fmt.Fprintf(os.Stderr, "warning: unable to read config: %v\n", err)
		}
	}
}
