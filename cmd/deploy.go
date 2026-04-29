package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
	"github.com/CharlieAIO/sol-cloud/internal/providers"
	"github.com/CharlieAIO/sol-cloud/internal/ui"
	"github.com/CharlieAIO/sol-cloud/internal/utils"
	"github.com/CharlieAIO/sol-cloud/internal/validator"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	deployRegion             string
	deployOrg                string
	deployDryRun             bool
	deploySkipHealthCheck    bool
	deployHealthCheckTimeout time.Duration
	deployHealthCheckPoll    time.Duration
	deploySlotsPerEpoch      uint64
	deployTicksPerSlot       uint64
	deployComputeUnitLimit   uint64
	deployLedgerLimitSize    uint64
	deployLedgerDiskLimitGB  int
	deployClonePrograms      []string
	deployCloneAccounts      []string
	deployCloneUpPrograms    []string
	deployAirdropRaw         []string // each entry: "ADDRESS" or "ADDRESS:AMOUNT"
	deployProgramSOPath      string
	deployProgramIDKeypair   string
	deployProgramIDLegacy    string
	deployUpgradeAuthority   string
	deployVolumeSize         int
	deploySkipVolume         bool
	deployForceReset         bool
	deployCloneRPCURL        string
)

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy a Solana validator",
	Long:  "Render deployment artifacts and deploy a Solana validator using the hidden project config.",
	Example: `  sol-cloud deploy
  sol-cloud deploy --dry-run
  sol-cloud deploy --region ord --health-timeout 4m
  sol-cloud deploy --slots-per-epoch 216000 --ticks-per-slot 32 --compute-unit-limit 300000
  sol-cloud deploy --clone 11111111111111111111111111111111 --clone-upgradeable-program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA
  sol-cloud deploy --program-so ./programs/my_program.so --program-id-keypair ./keys/program-keypair.json --upgrade-authority ./keys/upgrade-authority.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		providerName := strings.ToLower(strings.TrimSpace(viper.GetString("provider")))
		if providerName == "" {
			providerName = "fly"
		}

		name := strings.TrimSpace(viper.GetString("app_name"))
		if name == "" {
			return fmt.Errorf("app_name is required in project config; run `sol-cloud init` first")
		}
		var err error
		switch providerName {
		case "fly":
			name, err = utils.EnsureFlyAppName(name)
			if err != nil {
				return fmt.Errorf("invalid app_name in project config: %w", err)
			}
		case "railway":
			name, err = utils.EnsureRailwayProjectName(name)
			if err != nil {
				return fmt.Errorf("invalid app_name in project config: %w", err)
			}
		default:
			return fmt.Errorf("unsupported provider %q: valid providers are fly, railway", providerName)
		}

		region := strings.TrimSpace(deployRegion)
		if region == "" {
			region = strings.TrimSpace(viper.GetString("region"))
		}
		if region == "" {
			if providerName == "railway" {
				region = "us-west"
			} else {
				region = "ord"
			}
		}

		projectDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		var airdropAccounts []validator.AirdropEntry
		_ = viper.UnmarshalKey("validator.airdrop_accounts", &airdropAccounts)

		validatorCfg := validator.Config{
			SlotsPerEpoch:            viper.GetUint64("validator.slots_per_epoch"),
			TicksPerSlot:             viper.GetUint64("validator.ticks_per_slot"),
			ComputeUnitLimit:         viper.GetUint64("validator.compute_unit_limit"),
			LedgerLimitSize:          viper.GetUint64("validator.ledger_limit_size"),
			LedgerDiskLimitGB:        viper.GetInt("validator.ledger_disk_limit_gb"),
			CloneRPCURL:              viper.GetString("validator.clone_rpc_url"),
			ClonePrograms:            viper.GetStringSlice("validator.clone_programs"),
			CloneAccounts:            viper.GetStringSlice("validator.clone_accounts"),
			CloneUpgradeablePrograms: viper.GetStringSlice("validator.clone_upgradeable_programs"),
			AirdropAccounts:          airdropAccounts,
			ForceReset:               viper.GetBool("validator.force_reset"),
			ProgramDeploy: validator.ProgramDeployConfig{
				SOPath:               viper.GetString("validator.program_deploy.so_path"),
				ProgramIDKeypairPath: viper.GetString("validator.program_deploy.program_id_keypair"),
				UpgradeAuthorityPath: viper.GetString("validator.program_deploy.upgrade_authority"),
			},
		}
		// Backward compatibility for older configs that used program_id pubkey semantics.
		if strings.TrimSpace(validatorCfg.ProgramDeploy.ProgramIDKeypairPath) == "" {
			validatorCfg.ProgramDeploy.ProgramIDKeypairPath = viper.GetString("validator.program_deploy.program_id")
		}
		validatorCfg.ApplyDefaults()
		if deploySlotsPerEpoch > 0 {
			validatorCfg.SlotsPerEpoch = deploySlotsPerEpoch
		}
		if deployTicksPerSlot > 0 {
			validatorCfg.TicksPerSlot = deployTicksPerSlot
		}
		if deployComputeUnitLimit > 0 {
			validatorCfg.ComputeUnitLimit = deployComputeUnitLimit
		}
		if deployLedgerLimitSize > 0 {
			validatorCfg.LedgerLimitSize = deployLedgerLimitSize
		}
		if deployLedgerDiskLimitGB > 0 {
			validatorCfg.LedgerDiskLimitGB = deployLedgerDiskLimitGB
		}
		if cmd.Flags().Changed("clone-program") {
			validatorCfg.ClonePrograms = append([]string(nil), deployClonePrograms...)
		}
		if cmd.Flags().Changed("clone") {
			validatorCfg.CloneAccounts = append([]string(nil), deployCloneAccounts...)
		}
		if cmd.Flags().Changed("clone-upgradeable-program") {
			validatorCfg.CloneUpgradeablePrograms = append([]string(nil), deployCloneUpPrograms...)
		}
		if cmd.Flags().Changed("program-so") {
			validatorCfg.ProgramDeploy.SOPath = deployProgramSOPath
		}
		if cmd.Flags().Changed("program-id-keypair") {
			validatorCfg.ProgramDeploy.ProgramIDKeypairPath = deployProgramIDKeypair
		}
		if cmd.Flags().Changed("program-id") {
			validatorCfg.ProgramDeploy.ProgramIDKeypairPath = deployProgramIDLegacy
		}
		if cmd.Flags().Changed("upgrade-authority") {
			validatorCfg.ProgramDeploy.UpgradeAuthorityPath = deployUpgradeAuthority
		}
		if cmd.Flags().Changed("airdrop") {
			parsed, parseErr := parseAirdropFlags(deployAirdropRaw)
			if parseErr != nil {
				return fmt.Errorf("invalid --airdrop flag: %w", parseErr)
			}
			validatorCfg.AirdropAccounts = parsed
		}
		if deployForceReset {
			validatorCfg.ForceReset = true
		}
		if cmd.Flags().Changed("clone-rpc-url") {
			validatorCfg.CloneRPCURL = deployCloneRPCURL
		}
		if err := validatorCfg.Validate(); err != nil {
			return fmt.Errorf("invalid validator config: %w", err)
		}

		volumeSize := deployVolumeSize
		if volumeSize <= 0 {
			volumeSize = 10
		}

		cfg := &providers.Config{
			Name:                name,
			OrgSlug:             firstNonEmpty(strings.TrimSpace(deployOrg), strings.TrimSpace(viper.GetString("org"))),
			Region:              region,
			ProjectDir:          projectDir,
			Validator:           validatorCfg,
			DryRun:              deployDryRun,
			SkipHealthCheck:     deploySkipHealthCheck,
			HealthCheckTimeout:  deployHealthCheckTimeout,
			HealthCheckInterval: deployHealthCheckPoll,
			VolumeSize:          volumeSize,
			SkipVolume:          deploySkipVolume,
		}

		provider, err := providers.NewProvider(providerName)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		totalSteps := 2
		if !deployDryRun {
			totalSteps = 7
			if providerName == "railway" {
				totalSteps = 8
			}
		}
		progress := ui.NewProgress(out, totalSteps)
		cfg.Reporter = progress
		progress.Start("Preparing deploy")

		var state *appconfig.State
		var operation appconfig.OperationRecord
		if !deployDryRun {
			progress.Step("Saving deploy operation state")
			state, err = appconfig.LoadState(projectDir)
			if err != nil {
				progress.Fail("Deploy failed")
				return fmt.Errorf("load local deployment state: %w", err)
			}
			operation, err = state.StartOperation("deploy", name, providerName, "deploy started")
			if err != nil {
				progress.Fail("Deploy failed")
				return fmt.Errorf("start deploy operation state: %w", err)
			}
			if err := appconfig.SaveState(projectDir, state); err != nil {
				progress.Fail("Deploy failed")
				return fmt.Errorf("save deploy operation state: %w", err)
			}
		}

		deployment, err := provider.Deploy(cmd.Context(), cfg)
		if err != nil {
			progress.Fail("Deploy failed")
			if state != nil && operation.ID != "" {
				_ = state.FinishOperation(operation.ID, "failed", err.Error())
				_ = appconfig.SaveState(projectDir, state)
			}
			return err
		}

		if deployDryRun {
			progress.Success("Dry run complete")
			ui.Header(out, "Dry Run")
			ui.Fields(out,
				ui.Field{Label: "App", Value: deployment.Name},
				ui.Field{Label: "Provider", Value: deployment.Provider},
				ui.Field{Label: "Artifacts", Value: deployment.ArtifactsDir},
				ui.Field{Label: "RPC", Value: deployment.RPCURL},
				ui.Field{Label: "WebSocket", Value: deployment.WebSocketURL},
				ui.Field{Label: "Validator", Value: validatorSummary(validatorCfg)},
			)
			if validatorCfg.ProgramDeploy.Enabled() {
				ui.Fields(out,
					ui.Field{Label: "Program SO", Value: validatorCfg.ProgramDeploy.SOPath},
					ui.Field{Label: "Program ID", Value: validatorCfg.ProgramDeploy.ProgramIDKeypairPath},
					ui.Field{Label: "Authority", Value: validatorCfg.ProgramDeploy.UpgradeAuthorityPath},
				)
			}
			return nil
		}

		progress.Step("Saving deployment state")
		if err := state.UpsertDeployment(appconfig.DeploymentRecord{
			Name:         deployment.Name,
			Provider:     deployment.Provider,
			RPCURL:       deployment.RPCURL,
			WebSocketURL: deployment.WebSocketURL,
			Region:       region,
			ArtifactsDir: deployment.ArtifactsDir,
			DashboardURL: deployment.DashboardURL,
		}); err != nil {
			progress.Fail("Deploy failed")
			return fmt.Errorf("update local deployment state: %w", err)
		}
		if operation.ID != "" {
			if err := state.FinishOperation(operation.ID, "succeeded", "deploy completed"); err != nil {
				progress.Fail("Deploy failed")
				return fmt.Errorf("finish deploy operation state: %w", err)
			}
		}
		if err := appconfig.SaveState(projectDir, state); err != nil {
			progress.Fail("Deploy failed")
			return fmt.Errorf("save local deployment state: %w", err)
		}

		progress.Success("Validator deployed")
		ui.Header(out, "Deployment")
		ui.Fields(out,
			ui.Field{Label: "App", Value: deployment.Name},
			ui.Field{Label: "Provider", Value: deployment.Provider},
			ui.Field{Label: "RPC", Value: deployment.RPCURL},
			ui.Field{Label: "WebSocket", Value: deployment.WebSocketURL},
			ui.Field{Label: "Dashboard", Value: deployment.DashboardURL},
			ui.Field{Label: "Artifacts", Value: deployment.ArtifactsDir},
			ui.Field{Label: "State", Value: appconfig.StateFilePath(projectDir)},
			ui.Field{Label: "Validator", Value: validatorSummary(validatorCfg)},
			ui.Field{Label: "Solana CLI", Value: fmt.Sprintf("solana config set --url %s", deployment.RPCURL)},
		)
		if validatorCfg.ProgramDeploy.Enabled() {
			ui.Fields(out,
				ui.Field{Label: "Program SO", Value: validatorCfg.ProgramDeploy.SOPath},
				ui.Field{Label: "Program ID", Value: validatorCfg.ProgramDeploy.ProgramIDKeypairPath},
				ui.Field{Label: "Authority", Value: validatorCfg.ProgramDeploy.UpgradeAuthorityPath},
			)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)

	deployCmd.Flags().StringVar(&deployRegion, "region", "", "Fly region (overrides region in config)")
	deployCmd.Flags().StringVar(&deployOrg, "org", "", "Org/team identifier (fly: org slug; railway: team id)")
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "render files but skip API deployment")
	deployCmd.Flags().BoolVar(&deploySkipHealthCheck, "skip-health-check", false, "skip post-deploy RPC health validation")
	deployCmd.Flags().DurationVar(&deployHealthCheckTimeout, "health-timeout", 3*time.Minute, "maximum wait for RPC health")
	deployCmd.Flags().DurationVar(&deployHealthCheckPoll, "health-interval", 5*time.Second, "poll interval for RPC health checks")
	deployCmd.Flags().Uint64Var(&deploySlotsPerEpoch, "slots-per-epoch", 0, "override validator.slots_per_epoch")
	deployCmd.Flags().Uint64Var(&deployTicksPerSlot, "ticks-per-slot", 0, "override validator.ticks_per_slot")
	deployCmd.Flags().Uint64Var(&deployComputeUnitLimit, "compute-unit-limit", 0, "override validator.compute_unit_limit")
	deployCmd.Flags().Uint64Var(&deployLedgerLimitSize, "ledger-limit-size", 0, "override validator.ledger_limit_size")
	deployCmd.Flags().IntVar(&deployLedgerDiskLimitGB, "ledger-disk-limit-gb", 0, "override validator.ledger_disk_limit_gb; ledger resets when disk usage reaches this cap")
	deployCmd.Flags().StringSliceVar(&deployClonePrograms, "clone-program", nil, "program/account pubkey(s) to clone; type is auto-detected at runtime (upgradeable or plain)")
	deployCmd.Flags().StringSliceVar(&deployCloneAccounts, "clone", nil, "repeatable account pubkey(s) to pass as --clone to solana-test-validator")
	deployCmd.Flags().StringSliceVar(&deployCloneUpPrograms, "clone-upgradeable-program", nil, "repeatable program pubkey(s) to pass as --clone-upgradeable-program")
	deployCmd.Flags().StringVar(&deployProgramSOPath, "program-so", "", "path to .so file to deploy on validator startup (overrides validator.program_deploy.so_path)")
	deployCmd.Flags().StringVar(&deployProgramIDKeypair, "program-id-keypair", "", "path to program id keypair used with --program-id during startup deploy (overrides validator.program_deploy.program_id_keypair)")
	deployCmd.Flags().StringVar(&deployProgramIDLegacy, "program-id", "", "deprecated alias for --program-id-keypair")
	_ = deployCmd.Flags().MarkDeprecated("program-id", "use --program-id-keypair with a keypair path")
	deployCmd.Flags().StringVar(&deployUpgradeAuthority, "upgrade-authority", "", "path to upgrade authority keypair (overrides validator.program_deploy.upgrade_authority)")
	deployCmd.Flags().IntVar(&deployVolumeSize, "volume-size", 10, "size of persistent ledger volume in GB")
	deployCmd.Flags().BoolVar(&deploySkipVolume, "skip-volume", false, "skip volume creation, use ephemeral storage (data loss on restart)")
	deployCmd.Flags().BoolVarP(&deployForceReset, "reset", "r", false, "wipe the existing ledger on startup so --clone and other args take effect")
	deployCmd.Flags().StringArrayVar(&deployAirdropRaw, "airdrop", nil, `airdrop SOL on startup; format: ADDRESS or ADDRESS:AMOUNT (default amount: 1000); repeatable`)
	deployCmd.Flags().StringVar(&deployCloneRPCURL, "clone-rpc-url", "", "RPC endpoint for --clone fetches (default: mainnet-beta; use a private endpoint if rate-limited)")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func validatorSummary(cfg validator.Config) string {
	return fmt.Sprintf("slots_per_epoch=%d ticks_per_slot=%d compute_unit_limit=%d ledger_limit_size=%d ledger_disk_limit_gb=%d clone_programs=%d clone=%d clone_upgradeable_program=%d",
		cfg.SlotsPerEpoch,
		cfg.TicksPerSlot,
		cfg.ComputeUnitLimit,
		cfg.LedgerLimitSize,
		cfg.LedgerDiskLimitGB,
		len(cfg.ClonePrograms),
		len(cfg.CloneAccounts),
		len(cfg.CloneUpgradeablePrograms),
	)
}

// parseAirdropFlags converts raw --airdrop flag values ("ADDRESS" or "ADDRESS:AMOUNT")
// into validator.AirdropEntry slice. A missing amount defaults to validator.DefaultAirdropAmount.
func parseAirdropFlags(raw []string) ([]validator.AirdropEntry, error) {
	entries := make([]validator.AirdropEntry, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		addr, amountStr, hasColon := strings.Cut(s, ":")
		addr = strings.TrimSpace(addr)
		if addr == "" {
			return nil, fmt.Errorf("empty address in %q", s)
		}
		amount := validator.DefaultAirdropAmount
		if hasColon {
			n, err := strconv.ParseUint(strings.TrimSpace(amountStr), 10, 64)
			if err != nil || n == 0 {
				return nil, fmt.Errorf("invalid amount in %q: must be a positive integer", s)
			}
			amount = n
		}
		entries = append(entries, validator.AirdropEntry{Address: addr, Amount: amount})
	}
	return entries, nil
}
