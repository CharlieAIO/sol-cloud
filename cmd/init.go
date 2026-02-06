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
	initTicksPerSlot     uint64
	initComputeUnitLimit uint64
	initLedgerLimitSize  uint64
	initCloneAccounts    []string
	initCloneUpPrograms  []string
	initProgramSOPath    string
	initProgramIDKeypair string
	initProgramIDLegacy  string
	initUpgradeAuthority string
	initForce            bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a starter config file",
	Long:  "Create a .sol-cloud.yml starter file in the current directory.",
	Example: `  sol-cloud init
  sol-cloud init --app-name my-validator --region ord
  sol-cloud init --slots-per-epoch 216000 --ticks-per-slot 32 --compute-unit-limit 300000 --ledger-limit-size 10000
  sol-cloud init --clone 11111111111111111111111111111111 --clone-upgradeable-program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA
  sol-cloud init --program-so ./programs/my_program.so --program-id-keypair ./keys/program-keypair.json --upgrade-authority ./keys/upgrade-authority.json
  sol-cloud init --force`,
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
		if initTicksPerSlot > 0 {
			cfg.TicksPerSlot = initTicksPerSlot
		}
		if initComputeUnitLimit > 0 {
			cfg.ComputeUnitLimit = initComputeUnitLimit
		}
		if initLedgerLimitSize > 0 {
			cfg.LedgerLimitSize = initLedgerLimitSize
		}
		if initCloneAccounts != nil {
			cfg.CloneAccounts = append([]string(nil), initCloneAccounts...)
		}
		if initCloneUpPrograms != nil {
			cfg.CloneUpgradeablePrograms = append([]string(nil), initCloneUpPrograms...)
		}
		programIDKeypair := initProgramIDKeypair
		if strings.TrimSpace(programIDKeypair) == "" {
			programIDKeypair = initProgramIDLegacy
		}
		if initProgramSOPath != "" || programIDKeypair != "" || initUpgradeAuthority != "" {
			cfg.ProgramDeploy = validator.ProgramDeployConfig{
				SOPath:               initProgramSOPath,
				ProgramIDKeypairPath: programIDKeypair,
				UpgradeAuthorityPath: initUpgradeAuthority,
			}
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

		fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", file)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringVar(&initAppName, "app-name", "", "Default Fly app name to write into config")
	initCmd.Flags().StringVar(&initRegion, "region", "ord", "Default Fly region to write into config")
	initCmd.Flags().Uint64Var(&initSlotsPerEpoch, "slots-per-epoch", validator.DefaultSlotsPerEpoch, "validator slots per epoch")
	initCmd.Flags().Uint64Var(&initTicksPerSlot, "ticks-per-slot", validator.DefaultTicksPerSlot, "validator ticks per slot")
	initCmd.Flags().Uint64Var(&initComputeUnitLimit, "compute-unit-limit", validator.DefaultComputeUnitLimit, "validator compute unit limit")
	initCmd.Flags().Uint64Var(&initLedgerLimitSize, "ledger-limit-size", validator.DefaultLedgerLimitSize, "validator ledger limit size")
	initCmd.Flags().StringSliceVar(&initCloneAccounts, "clone", nil, "repeatable account pubkey(s) to add as validator.clone_accounts")
	initCmd.Flags().StringSliceVar(&initCloneUpPrograms, "clone-upgradeable-program", nil, "repeatable program pubkey(s) to add as validator.clone_upgradeable_programs")
	initCmd.Flags().StringVar(&initProgramSOPath, "program-so", "", "optional .so path to set validator.program_deploy.so_path")
	initCmd.Flags().StringVar(&initProgramIDKeypair, "program-id-keypair", "", "optional keypair path to set validator.program_deploy.program_id_keypair")
	initCmd.Flags().StringVar(&initProgramIDLegacy, "program-id", "", "deprecated alias for --program-id-keypair")
	_ = initCmd.Flags().MarkDeprecated("program-id", "use --program-id-keypair with a keypair path")
	initCmd.Flags().StringVar(&initUpgradeAuthority, "upgrade-authority", "", "optional keypair path to set validator.program_deploy.upgrade_authority")
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
