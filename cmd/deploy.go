package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/CharlieAIO/sol-cloud/internal/providers"
	"github.com/CharlieAIO/sol-cloud/internal/validator"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	deployName               string
	deployRegion             string
	deployDryRun             bool
	deploySkipHealthCheck    bool
	deployHealthCheckTimeout time.Duration
	deployHealthCheckPoll    time.Duration
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy a validator to Fly.io",
	Long:  "Render deployment artifacts and deploy a Solana validator to Fly.io.",
	RunE: func(cmd *cobra.Command, args []string) error {
		providerName := strings.ToLower(strings.TrimSpace(viper.GetString("provider")))
		if providerName == "" {
			providerName = "fly"
		}
		if providerName != "fly" {
			return fmt.Errorf("unsupported provider %q: only fly is enabled", providerName)
		}

		name := strings.TrimSpace(deployName)
		if name == "" {
			name = strings.TrimSpace(viper.GetString("app_name"))
		}
		if name == "" {
			return errors.New("app name is required (set --name or app_name in .sol-cloud.yml)")
		}

		region := strings.TrimSpace(deployRegion)
		if region == "" {
			region = strings.TrimSpace(viper.GetString("region"))
		}
		if region == "" {
			region = "ord"
		}

		projectDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		validatorCfg := validator.Config{
			SlotsPerEpoch:    viper.GetUint64("validator.slots_per_epoch"),
			ClockMultiplier:  viper.GetUint64("validator.clock_multiplier"),
			ComputeUnitLimit: viper.GetUint64("validator.compute_unit_limit"),
		}
		if validatorCfg.SlotsPerEpoch == 0 {
			validatorCfg.SlotsPerEpoch = 432000
		}
		if validatorCfg.ClockMultiplier == 0 {
			validatorCfg.ClockMultiplier = 1
		}
		if validatorCfg.ComputeUnitLimit == 0 {
			validatorCfg.ComputeUnitLimit = 200000
		}

		cfg := &providers.Config{
			Name:                name,
			Region:              region,
			ProjectDir:          projectDir,
			Validator:           validatorCfg,
			DryRun:              deployDryRun,
			SkipHealthCheck:     deploySkipHealthCheck,
			HealthCheckTimeout:  deployHealthCheckTimeout,
			HealthCheckInterval: deployHealthCheckPoll,
		}

		flyProvider := providers.NewFlyProvider()
		if !deployDryRun {
			fmt.Fprintln(cmd.OutOrStdout(), "deploying validator to Fly.io...")
		}
		deployment, err := flyProvider.Deploy(cmd.Context(), cfg)
		if err != nil {
			return err
		}

		if deployDryRun {
			fmt.Fprintln(cmd.OutOrStdout(), "dry run complete; deployment files generated")
			fmt.Fprintf(cmd.OutOrStdout(), "app:          %s\n", deployment.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "artifacts:    %s\n", deployment.ArtifactsDir)
			fmt.Fprintf(cmd.OutOrStdout(), "rpc endpoint: %s\n", deployment.RPCURL)
			fmt.Fprintf(cmd.OutOrStdout(), "ws endpoint:  %s\n", deployment.WebSocketURL)
			return nil
		}

		fmt.Fprintln(cmd.OutOrStdout(), "validator deployed")
		fmt.Fprintf(cmd.OutOrStdout(), "app:          %s\n", deployment.Name)
		fmt.Fprintf(cmd.OutOrStdout(), "rpc endpoint: %s\n", deployment.RPCURL)
		fmt.Fprintf(cmd.OutOrStdout(), "ws endpoint:  %s\n", deployment.WebSocketURL)
		fmt.Fprintf(cmd.OutOrStdout(), "artifacts:    %s\n", deployment.ArtifactsDir)
		fmt.Fprintf(cmd.OutOrStdout(), "tip:          solana config set --url %s\n", deployment.RPCURL)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)

	deployCmd.Flags().StringVar(&deployName, "name", "", "Fly app name (overrides app_name in config)")
	deployCmd.Flags().StringVar(&deployRegion, "region", "", "Fly region (overrides region in config)")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "render files but skip flyctl deployment")
	deployCmd.Flags().BoolVar(&deploySkipHealthCheck, "skip-health-check", false, "skip post-deploy RPC health validation")
	deployCmd.Flags().DurationVar(&deployHealthCheckTimeout, "health-timeout", 3*time.Minute, "maximum wait for RPC health")
	deployCmd.Flags().DurationVar(&deployHealthCheckPoll, "health-interval", 5*time.Second, "poll interval for RPC health checks")
}
