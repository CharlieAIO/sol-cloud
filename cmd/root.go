package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "sol-cloud",
	Short: "Deploy Solana validators to cloud providers",
	Long:  "sol-cloud deploys and manages Solana validators on cloud providers like Fly.io.",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .sol-cloud.yml)")
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.SetConfigType("yaml")
		viper.SetConfigName(".sol-cloud")
		viper.AddConfigPath(".")

		home, err := os.UserHomeDir()
		if err == nil {
			viper.AddConfigPath(filepath.Join(home, ".config", "sol-cloud"))
		}
	}

	viper.SetEnvPrefix("SOL_CLOUD")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			fmt.Fprintf(os.Stderr, "warning: unable to read config: %v\n", err)
		}
	}
}
