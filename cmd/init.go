package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
	"github.com/CharlieAIO/sol-cloud/internal/ui"
	"github.com/CharlieAIO/sol-cloud/internal/utils"
	"github.com/CharlieAIO/sol-cloud/internal/validator"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	initForce    bool
	initYes      bool
	initProvider string
)

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
	Short: "Run interactive setup and create a hidden project config",
	Long:  "Run an interactive setup flow and write a hidden per-project config outside the current directory.",
	Example: `  sol-cloud init
  sol-cloud init --force
  sol-cloud init --yes
  sol-cloud init --yes --provider railway`,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		reader := bufio.NewReader(cmd.InOrStdin())

		projectDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		file, err := appconfig.ProjectConfigPath(projectDir)
		if err != nil {
			return fmt.Errorf("resolve project config path: %w", err)
		}

		if _, err := os.Stat(file); err == nil {
			if !initForce && !initYes {
				overwrite, promptErr := utils.YesNo(reader, out, "Hidden project config already exists. Overwrite it?", false)
				if promptErr != nil {
					return promptErr
				}
				if !overwrite {
					fmt.Fprintln(out, "init cancelled; existing project config was not changed")
					return nil
				}
			}
		}

		// --yes: skip all prompts, generate app name, use defaults.
		if initYes {
			providerName := strings.ToLower(strings.TrimSpace(initProvider))
			if providerName == "" {
				providerName = "fly"
			}
			if providerName != "fly" && providerName != "railway" {
				return fmt.Errorf("unsupported provider %q: valid providers are fly, railway", providerName)
			}

			var appName, region string
			var genErr error
			switch providerName {
			case "railway":
				appName, genErr = utils.GenerateRailwayProjectName()
				region = "us-west"
			default:
				appName, genErr = utils.GenerateFlyAppName()
				region = "ord"
			}
			if genErr != nil {
				return genErr
			}

			cfg := validator.DefaultConfig()
			return writeInitConfig(out, file, providerName, appName, region, cfg)
		}

		ui.Header(out, "Sol-Cloud Setup")
		fmt.Fprintln(out, "Press Enter to accept defaults.")
		fmt.Fprintln(out)

		providerName := strings.ToLower(strings.TrimSpace(initProvider))
		if providerName == "" {
			providerName, err = utils.SelectOptionArrow(cmd.InOrStdin(), out, "Provider", providerOptions, "fly")
			if err != nil {
				// Fall back to prompt if terminal is not interactive.
				providerName, err = utils.String(reader, out, "Provider (fly or railway)", "fly", true)
				if err != nil {
					return err
				}
				providerName = strings.ToLower(strings.TrimSpace(providerName))
			}
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
			ledgerDiskLimit, promptErr := utils.Uint64(reader, out, "Ledger disk limit GB", uint64(cfg.LedgerDiskLimitGB), true)
			if promptErr != nil {
				return promptErr
			}
			cfg.LedgerDiskLimitGB = int(ledgerDiskLimit)
		}

		clonePrograms, err := utils.StringList(reader, out, "Clone programs/accounts (optional, type auto-detected at runtime)", "Program/account pubkey")
		if err != nil {
			return err
		}
		cfg.ClonePrograms = clonePrograms

		airdropEntries, err := promptAirdropAccounts(reader, out)
		if err != nil {
			return err
		}
		cfg.AirdropAccounts = airdropEntries

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

		return writeInitConfig(out, file, providerName, appName, region, cfg)
	},
}

func writeInitConfig(out interface{ Write([]byte) (int, error) }, file, providerName, appName, region string, cfg validator.Config) error {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid setup values: %w", err)
	}

	escapedAppName := strings.ReplaceAll(appName, `"`, `\"`)
	escapedRegion := strings.ReplaceAll(region, `"`, `\"`)
	cloneProgramsYAML := renderYAMLStringList(cfg.ClonePrograms, "    ")
	airdropYAML := renderYAMLAirdropList(cfg.AirdropAccounts, "    ")
	escapedSOPath := strings.ReplaceAll(cfg.ProgramDeploy.SOPath, `"`, `\"`)
	escapedProgramIDKeypair := strings.ReplaceAll(cfg.ProgramDeploy.ProgramIDKeypairPath, `"`, `\"`)
	escapedUpgradeAuth := strings.ReplaceAll(cfg.ProgramDeploy.UpgradeAuthorityPath, `"`, `\"`)

	content := fmt.Sprintf(`provider: %s
app_name: "%s"
region: "%s"
validator:
  slots_per_epoch: %d
  ticks_per_slot: %d
  compute_unit_limit: %d
  ledger_limit_size: %d
  ledger_disk_limit_gb: %d
  clone_programs:
%s
  airdrop_accounts:
%s
  program_deploy:
    so_path: "%s"
    program_id_keypair: "%s"
    upgrade_authority: "%s"
`, providerName, escapedAppName, escapedRegion, cfg.SlotsPerEpoch, cfg.TicksPerSlot, cfg.ComputeUnitLimit, cfg.LedgerLimitSize, cfg.LedgerDiskLimitGB, cloneProgramsYAML, airdropYAML, escapedSOPath, escapedProgramIDKeypair, escapedUpgradeAuth)

	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return fmt.Errorf("create project config directory: %w", err)
	}
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	viper.SetConfigFile(file)
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("reload project config: %w", err)
	}

	ui.Header(out, "Config")
	ui.Fields(out,
		ui.Field{Label: "File", Value: file},
		ui.Field{Label: "Provider", Value: providerName},
		ui.Field{Label: "App", Value: appName},
		ui.Field{Label: "Region", Value: region},
	)
	return nil
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite hidden project config if it already exists")
	initCmd.Flags().BoolVarP(&initYes, "yes", "y", false, "Skip all prompts; write config with generated app name and validator defaults")
	initCmd.Flags().StringVar(&initProvider, "provider", "", "Provider to use (fly or railway); defaults to fly when --yes is set")
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

func promptAirdropAccounts(reader *bufio.Reader, out io.Writer) ([]validator.AirdropEntry, error) {
	fmt.Fprintln(out, "Airdrop accounts (optional)")
	fmt.Fprintln(out, "Press Enter on an empty address to finish.")

	var entries []validator.AirdropEntry
	for {
		address, err := utils.String(reader, out, fmt.Sprintf("Airdrop address #%d", len(entries)+1), "", false)
		if err != nil {
			return nil, err
		}
		address = strings.TrimSpace(address)
		if address == "" {
			return entries, nil
		}
		amount, err := utils.Uint64(reader, out, fmt.Sprintf("  Amount (SOL) for %s", address), validator.DefaultAirdropAmount, true)
		if err != nil {
			return nil, err
		}
		entries = append(entries, validator.AirdropEntry{Address: address, Amount: amount})
	}
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

func renderYAMLAirdropList(entries []validator.AirdropEntry, indent string) string {
	if len(entries) == 0 {
		return indent + "[]"
	}
	lines := make([]string, 0, len(entries)*2)
	for _, e := range entries {
		escaped := strings.ReplaceAll(e.Address, `"`, `\"`)
		lines = append(lines,
			fmt.Sprintf("%s- address: \"%s\"", indent, escaped),
			fmt.Sprintf("%s  amount: %d", indent, e.Amount),
		)
	}
	return strings.Join(lines, "\n")
}
