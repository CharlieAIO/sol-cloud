package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/CharlieAIO/sol-cloud/internal/validator"
	"github.com/spf13/cobra"
)

var (
	initAppName          string
	initRegion           string
	initSlotsPerEpoch    uint64
	initClockMultiplier  uint64
	initComputeUnitLimit uint64
	initForce            bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a starter config file",
	Long:  "Create a .sol-cloud.yml starter file in the current directory.",
	RunE: func(cmd *cobra.Command, args []string) error {
		const file = ".sol-cloud.yml"
		if _, err := os.Stat(file); err == nil && !initForce {
			fmt.Fprintln(cmd.OutOrStdout(), ".sol-cloud.yml already exists (use --force to overwrite)")
			return nil
		}

		cfg := validator.DefaultConfig()
		if initSlotsPerEpoch > 0 {
			cfg.SlotsPerEpoch = initSlotsPerEpoch
		}
		if initClockMultiplier > 0 {
			cfg.ClockMultiplier = initClockMultiplier
		}
		if initComputeUnitLimit > 0 {
			cfg.ComputeUnitLimit = initComputeUnitLimit
		}
		cfg.ApplyDefaults()
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid validator config: %w", err)
		}

		region := strings.TrimSpace(initRegion)
		if region == "" {
			region = "ord"
		}
		appName := strings.TrimSpace(initAppName)
		escapedAppName := strings.ReplaceAll(appName, `"`, `\"`)
		escapedRegion := strings.ReplaceAll(region, `"`, `\"`)

		starter := fmt.Sprintf(`provider: fly
app_name: "%s"
region: "%s"
validator:
  slots_per_epoch: %d
  clock_multiplier: %d
  compute_unit_limit: %d
`, escapedAppName, escapedRegion, cfg.SlotsPerEpoch, cfg.ClockMultiplier, cfg.ComputeUnitLimit)

		if err := os.WriteFile(file, []byte(starter), 0o644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", file)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringVar(&initAppName, "app-name", "", "Default Fly app name to write into config")
	initCmd.Flags().StringVar(&initRegion, "region", "ord", "Default Fly region to write into config")
	initCmd.Flags().Uint64Var(&initSlotsPerEpoch, "slots-per-epoch", validator.DefaultSlotsPerEpoch, "validator slots per epoch")
	initCmd.Flags().Uint64Var(&initClockMultiplier, "clock-multiplier", validator.DefaultClockMultiplier, "validator clock multiplier")
	initCmd.Flags().Uint64Var(&initComputeUnitLimit, "compute-unit-limit", validator.DefaultComputeUnitLimit, "validator compute unit limit")
	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite .sol-cloud.yml if it already exists")
}
