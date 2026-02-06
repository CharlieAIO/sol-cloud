package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/CharlieAIO/sol-cloud/internal/validator"
	"github.com/spf13/cobra"
)

var initForce bool

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
				overwrite, promptErr := promptYesNo(reader, out, ".sol-cloud.yml already exists. Overwrite it?", false)
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

		appName, err := promptString(reader, out, "Fly app name", defaultAppName(), true)
		if err != nil {
			return err
		}
		region, err := promptString(reader, out, "Fly region", "ord", true)
		if err != nil {
			return err
		}

		cfg := validator.DefaultConfig()
		customizeRuntime, err := promptYesNo(reader, out, "Customize validator runtime settings?", false)
		if err != nil {
			return err
		}
		if customizeRuntime {
			cfg.SlotsPerEpoch, err = promptUint64(reader, out, "Slots per epoch", cfg.SlotsPerEpoch, true)
			if err != nil {
				return err
			}
			cfg.TicksPerSlot, err = promptUint64(reader, out, "Ticks per slot", cfg.TicksPerSlot, true)
			if err != nil {
				return err
			}
			cfg.ComputeUnitLimit, err = promptUint64(reader, out, "Compute unit limit", cfg.ComputeUnitLimit, true)
			if err != nil {
				return err
			}
			cfg.LedgerLimitSize, err = promptUint64(reader, out, "Ledger limit size", cfg.LedgerLimitSize, true)
			if err != nil {
				return err
			}
		}

		cloneAccountsRaw, err := promptString(reader, out, "Clone account list (comma-separated, optional)", "", false)
		if err != nil {
			return err
		}
		cfg.CloneAccounts = parseCSVList(cloneAccountsRaw)

		cloneProgramsRaw, err := promptString(reader, out, "Clone upgradeable program list (comma-separated, optional)", "", false)
		if err != nil {
			return err
		}
		cfg.CloneUpgradeablePrograms = parseCSVList(cloneProgramsRaw)

		configureStartupDeploy, err := promptYesNo(reader, out, "Configure startup program deploy?", false)
		if err != nil {
			return err
		}
		if configureStartupDeploy {
			soPath, promptErr := promptString(reader, out, "Program .so path", "", true)
			if promptErr != nil {
				return promptErr
			}
			programIDKeypair, promptErr := promptString(reader, out, "Program ID keypair path", "", true)
			if promptErr != nil {
				return promptErr
			}
			upgradeAuthorityPath, promptErr := promptString(reader, out, "Upgrade authority keypair path", "", true)
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

		starter := fmt.Sprintf(`provider: fly
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
`, escapedAppName, escapedRegion, cfg.SlotsPerEpoch, cfg.TicksPerSlot, cfg.ComputeUnitLimit, cfg.LedgerLimitSize, cloneAccountsYAML, cloneProgramsYAML, escapedSOPath, escapedProgramIDKeypair, escapedUpgradeAuth)

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

func parseCSVList(input string) []string {
	if strings.TrimSpace(input) == "" {
		return []string{}
	}
	parts := strings.Split(input, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}

func promptString(reader *bufio.Reader, out io.Writer, label, defaultValue string, required bool) (string, error) {
	for {
		if strings.TrimSpace(defaultValue) == "" {
			fmt.Fprintf(out, "%s: ", label)
		} else {
			fmt.Fprintf(out, "%s [%s]: ", label, defaultValue)
		}

		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}

		value := strings.TrimSpace(line)
		if value == "" {
			value = strings.TrimSpace(defaultValue)
		}
		if required && value == "" {
			fmt.Fprintln(out, "value is required")
			if err == io.EOF {
				return "", io.ErrUnexpectedEOF
			}
			continue
		}
		return value, nil
	}
}

func promptYesNo(reader *bufio.Reader, out io.Writer, label string, defaultYes bool) (bool, error) {
	defaultLabel := "y/N"
	if defaultYes {
		defaultLabel = "Y/n"
	}
	for {
		fmt.Fprintf(out, "%s [%s]: ", label, defaultLabel)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return false, err
		}

		answer := strings.ToLower(strings.TrimSpace(line))
		switch answer {
		case "":
			return defaultYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "enter y or n")
			if err == io.EOF {
				return false, io.ErrUnexpectedEOF
			}
		}
	}
}

func promptUint64(reader *bufio.Reader, out io.Writer, label string, defaultValue uint64, required bool) (uint64, error) {
	for {
		value, err := promptString(reader, out, label, strconv.FormatUint(defaultValue, 10), required)
		if err != nil {
			return 0, err
		}
		if value == "" && !required {
			return 0, nil
		}

		parsed, parseErr := strconv.ParseUint(value, 10, 64)
		if parseErr != nil {
			fmt.Fprintln(out, "enter a valid positive integer")
			continue
		}
		return parsed, nil
	}
}

func defaultAppName() string {
	wd, err := os.Getwd()
	if err != nil {
		return "sol-cloud-validator"
	}

	base := strings.ToLower(filepath.Base(wd))
	var b strings.Builder
	lastDash := false
	for _, r := range base {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}

	clean := strings.Trim(b.String(), "-")
	if clean == "" {
		return "sol-cloud-validator"
	}
	if len(clean) > 40 {
		clean = clean[:40]
		clean = strings.Trim(clean, "-")
	}
	if clean == "" {
		return "sol-cloud-validator"
	}
	return clean
}
