package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/CharlieAIO/sol-cloud/internal/utils"
	"github.com/CharlieAIO/sol-cloud/internal/validator"
	"github.com/spf13/cobra"
)

var initForce bool

var providerOptions = []utils.Option{
	{Key: "fly", Label: "Fly.io"},
	{Key: "railway", Label: "Railway"},
}

var railwayRegionOptions = []utils.Option{
	{Key: "us-west", Label: "US West"},
	{Key: "us-east", Label: "US East"},
	{Key: "eu-west", Label: "EU West"},
	{Key: "asia-southeast", Label: "Asia Southeast"},
}

var flyRegionOptions = []utils.Option{
	{Key: "ord", Label: "Chicago"},
	{Key: "iad", Label: "Ashburn"},
	{Key: "dfw", Label: "Dallas"},
	{Key: "lax", Label: "Los Angeles"},
	{Key: "sjc", Label: "San Jose"},
	{Key: "sea", Label: "Seattle"},
	{Key: "mia", Label: "Miami"},
	{Key: "atl", Label: "Atlanta"},
	{Key: "bos", Label: "Boston"},
	{Key: "yyz", Label: "Toronto"},
	{Key: "gru", Label: "Sao Paulo"},
	{Key: "eze", Label: "Buenos Aires"},
	{Key: "scl", Label: "Santiago"},
	{Key: "lhr", Label: "London"},
	{Key: "ams", Label: "Amsterdam"},
	{Key: "fra", Label: "Frankfurt"},
	{Key: "mad", Label: "Madrid"},
	{Key: "cdg", Label: "Paris"},
	{Key: "waw", Label: "Warsaw"},
	{Key: "otp", Label: "Bucharest"},
	{Key: "bom", Label: "Mumbai"},
	{Key: "sin", Label: "Singapore"},
	{Key: "nrt", Label: "Tokyo"},
	{Key: "hkg", Label: "Hong Kong"},
	{Key: "syd", Label: "Sydney"},
	{Key: "jnb", Label: "Johannesburg"},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Run interactive setup and create .sol-cloud.yml",
	Long:  "Run an interactive setup flow and write a .sol-cloud.yml file in the current directory.",
	Example: `  sol-cloud init
  sol-cloud init --force`,
	RunE: func(cmd *cobra.Command, args []string) error {
		const file = ".sol-cloud.yml"
		out := cmd.OutOrStdout()
		reader := bufio.NewReader(cmd.InOrStdin())

		if _, err := os.Stat(file); err == nil {
			if !initForce {
				overwrite, promptErr := utils.YesNo(reader, out, ".sol-cloud.yml already exists. Overwrite it?", false)
				if promptErr != nil {
					return promptErr
				}
				if !overwrite {
					fmt.Fprintln(out, "init cancelled; existing .sol-cloud.yml was not changed")
					return nil
				}
			}
		}

		fmt.Fprintln(out, "Sol-Cloud setup")
		fmt.Fprintln(out, "Press Enter to accept defaults.")
		fmt.Fprintln(out)

		providerName, err := utils.SelectOptionArrow(cmd.InOrStdin(), out, "Provider", providerOptions, "fly")
		if err != nil {
			// Fall back to prompt if terminal is not interactive.
			providerName, err = utils.String(reader, out, "Provider (fly or railway)", "fly", true)
			if err != nil {
				return err
			}
			providerName = strings.ToLower(strings.TrimSpace(providerName))
		}

		var appName string
		var region string
		switch providerName {
		case "railway":
			appName, err = utils.GenerateRailwayProjectName()
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Railway project name: %s\n", appName)
			region, err = promptRailwayRegion(cmd.InOrStdin(), reader, out, "us-west")
			if err != nil {
				return err
			}
		default:
			providerName = "fly"
			appName, err = utils.GenerateFlyAppName()
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Fly app name: %s\n", appName)
			region, err = promptFlyRegion(cmd.InOrStdin(), reader, out, "ord")
			if err != nil {
				return err
			}
		}

		cfg := validator.DefaultConfig()
		customizeRuntime, err := utils.YesNo(reader, out, "Customize validator runtime settings?", false)
		if err != nil {
			return err
		}
		if customizeRuntime {
			cfg.SlotsPerEpoch, err = utils.Uint64(reader, out, "Slots per epoch", cfg.SlotsPerEpoch, true)
			if err != nil {
				return err
			}
			cfg.TicksPerSlot, err = utils.Uint64(reader, out, "Ticks per slot", cfg.TicksPerSlot, true)
			if err != nil {
				return err
			}
			cfg.ComputeUnitLimit, err = utils.Uint64(reader, out, "Compute unit limit", cfg.ComputeUnitLimit, true)
			if err != nil {
				return err
			}
			cfg.LedgerLimitSize, err = utils.Uint64(reader, out, "Ledger limit size", cfg.LedgerLimitSize, true)
			if err != nil {
				return err
			}
		}

		cloneAccounts, err := utils.StringList(reader, out, "Clone account list (optional)", "Clone account")
		if err != nil {
			return err
		}
		cfg.CloneAccounts = cloneAccounts

		clonePrograms, err := utils.StringList(reader, out, "Clone upgradeable program list (optional)", "Clone upgradeable program")
		if err != nil {
			return err
		}
		cfg.CloneUpgradeablePrograms = clonePrograms

		configureStartupDeploy, err := utils.YesNo(reader, out, "Configure startup program deploy?", false)
		if err != nil {
			return err
		}
		if configureStartupDeploy {
			soPath, promptErr := utils.String(reader, out, "Program .so path", "", true)
			if promptErr != nil {
				return promptErr
			}
			programIDKeypair, promptErr := utils.String(reader, out, "Program ID keypair path", "", true)
			if promptErr != nil {
				return promptErr
			}
			upgradeAuthorityPath, promptErr := utils.String(reader, out, "Upgrade authority keypair path", "", true)
			if promptErr != nil {
				return promptErr
			}
			cfg.ProgramDeploy = validator.ProgramDeployConfig{
				SOPath:               soPath,
				ProgramIDKeypairPath: programIDKeypair,
				UpgradeAuthorityPath: upgradeAuthorityPath,
			}
		}

		cfg.ApplyDefaults()
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("invalid setup values: %w", err)
		}

		escapedAppName := strings.ReplaceAll(appName, `"`, `\"`)
		escapedRegion := strings.ReplaceAll(region, `"`, `\"`)
		cloneAccountsYAML := renderYAMLStringList(cfg.CloneAccounts, "    ")
		cloneProgramsYAML := renderYAMLStringList(cfg.CloneUpgradeablePrograms, "    ")
		escapedSOPath := strings.ReplaceAll(cfg.ProgramDeploy.SOPath, `"`, `\"`)
		escapedProgramIDKeypair := strings.ReplaceAll(cfg.ProgramDeploy.ProgramIDKeypairPath, `"`, `\"`)
		escapedUpgradeAuth := strings.ReplaceAll(cfg.ProgramDeploy.UpgradeAuthorityPath, `"`, `\"`)

		starter := fmt.Sprintf(`provider: %s
app_name: "%s"
region: "%s"
validator:
  slots_per_epoch: %d
  ticks_per_slot: %d
  compute_unit_limit: %d
  ledger_limit_size: %d
  clone_accounts:
%s
  clone_upgradeable_programs:
%s
  program_deploy:
    so_path: "%s"
    program_id_keypair: "%s"
    upgrade_authority: "%s"
`, providerName, escapedAppName, escapedRegion, cfg.SlotsPerEpoch, cfg.TicksPerSlot, cfg.ComputeUnitLimit, cfg.LedgerLimitSize, cloneAccountsYAML, cloneProgramsYAML, escapedSOPath, escapedProgramIDKeypair, escapedUpgradeAuth)

		if err := os.WriteFile(file, []byte(starter), 0o644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}

		fmt.Fprintf(out, "created %s\n", file)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite .sol-cloud.yml if it already exists")
}

func promptFlyRegion(in io.Reader, reader *bufio.Reader, out io.Writer, defaultRegion string) (string, error) {
	defaultRegion = strings.ToLower(strings.TrimSpace(defaultRegion))
	if defaultRegion == "" {
		defaultRegion = "ord"
	}

	region, err := utils.SelectOptionArrow(in, out, "Fly region", flyRegionOptions, defaultRegion)
	if err == nil {
		return region, nil
	}
	if errors.Is(err, utils.ErrManualSelection) {
		custom, promptErr := utils.String(reader, out, "Custom Fly region code", defaultRegion, true)
		if promptErr != nil {
			return "", promptErr
		}
		return strings.ToLower(strings.TrimSpace(custom)), nil
	}

	custom, promptErr := utils.String(reader, out, "Fly region code", defaultRegion, true)
	if promptErr != nil {
		return "", promptErr
	}
	return strings.ToLower(strings.TrimSpace(custom)), nil
}

func promptRailwayRegion(in io.Reader, reader *bufio.Reader, out io.Writer, defaultRegion string) (string, error) {
	defaultRegion = strings.ToLower(strings.TrimSpace(defaultRegion))
	if defaultRegion == "" {
		defaultRegion = "us-west"
	}

	region, err := utils.SelectOptionArrow(in, out, "Railway region", railwayRegionOptions, defaultRegion)
	if err == nil {
		return region, nil
	}
	if errors.Is(err, utils.ErrManualSelection) {
		custom, promptErr := utils.String(reader, out, "Custom Railway region code", defaultRegion, true)
		if promptErr != nil {
			return "", promptErr
		}
		return strings.ToLower(strings.TrimSpace(custom)), nil
	}

	custom, promptErr := utils.String(reader, out, "Railway region code", defaultRegion, true)
	if promptErr != nil {
		return "", promptErr
	}
	return strings.ToLower(strings.TrimSpace(custom)), nil
}

func renderYAMLStringList(values []string, indent string) string {
	if len(values) == 0 {
		return indent + "[]"
	}
	lines := make([]string, 0, len(values))
	for _, value := range values {
		escaped := strings.ReplaceAll(value, `"`, `\"`)
		lines = append(lines, fmt.Sprintf("%s- \"%s\"", indent, escaped))
	}
	return strings.Join(lines, "\n")
}
