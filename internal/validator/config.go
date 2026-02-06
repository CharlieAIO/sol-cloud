package validator

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	DefaultSlotsPerEpoch    uint64 = 432000
	DefaultTicksPerSlot     uint64 = 64
	DefaultComputeUnitLimit uint64 = 200000
	DefaultLedgerLimitSize  uint64 = 10000

	minSlotsPerEpoch    uint64 = 32
	maxTicksPerSlot     uint64 = 1024
	minComputeUnitLimit uint64 = 10000
	minLedgerLimitSize  uint64 = 1
)

var base58AddressPattern = regexp.MustCompile(`^[1-9A-HJ-NP-Za-km-z]{32,44}$`)

// Config holds runtime validator parameters.
type Config struct {
	SlotsPerEpoch            uint64              `mapstructure:"slots_per_epoch" yaml:"slots_per_epoch"`
	TicksPerSlot             uint64              `mapstructure:"ticks_per_slot" yaml:"ticks_per_slot"`
	ComputeUnitLimit         uint64              `mapstructure:"compute_unit_limit" yaml:"compute_unit_limit"`
	LedgerLimitSize          uint64              `mapstructure:"ledger_limit_size" yaml:"ledger_limit_size"`
	CloneAccounts            []string            `mapstructure:"clone_accounts" yaml:"clone_accounts"`
	CloneUpgradeablePrograms []string            `mapstructure:"clone_upgradeable_programs" yaml:"clone_upgradeable_programs"`
	ProgramDeploy            ProgramDeployConfig `mapstructure:"program_deploy" yaml:"program_deploy"`
}

// ProgramDeployConfig configures optional startup program deployment.
type ProgramDeployConfig struct {
	SOPath               string `mapstructure:"so_path" yaml:"so_path"`
	ProgramIDKeypairPath string `mapstructure:"program_id_keypair" yaml:"program_id_keypair"`
	UpgradeAuthorityPath string `mapstructure:"upgrade_authority" yaml:"upgrade_authority"`
}

// HasValues returns true when any program deploy field is configured.
func (p ProgramDeployConfig) HasValues() bool {
	return strings.TrimSpace(p.SOPath) != "" ||
		strings.TrimSpace(p.ProgramIDKeypairPath) != "" ||
		strings.TrimSpace(p.UpgradeAuthorityPath) != ""
}

// Enabled returns true when all required startup deploy fields are configured.
func (p ProgramDeployConfig) Enabled() bool {
	return strings.TrimSpace(p.SOPath) != "" &&
		strings.TrimSpace(p.ProgramIDKeypairPath) != "" &&
		strings.TrimSpace(p.UpgradeAuthorityPath) != ""
}

// DefaultConfig returns the baseline validator configuration.
func DefaultConfig() Config {
	return Config{
		SlotsPerEpoch:            DefaultSlotsPerEpoch,
		TicksPerSlot:             DefaultTicksPerSlot,
		ComputeUnitLimit:         DefaultComputeUnitLimit,
		LedgerLimitSize:          DefaultLedgerLimitSize,
		CloneAccounts:            []string{},
		CloneUpgradeablePrograms: []string{},
		ProgramDeploy:            ProgramDeployConfig{},
	}
}

// ApplyDefaults fills in zero values with defaults.
func (c *Config) ApplyDefaults() {
	if c == nil {
		return
	}
	if c.SlotsPerEpoch == 0 {
		c.SlotsPerEpoch = DefaultSlotsPerEpoch
	}
	if c.TicksPerSlot == 0 {
		c.TicksPerSlot = DefaultTicksPerSlot
	}
	if c.ComputeUnitLimit == 0 {
		c.ComputeUnitLimit = DefaultComputeUnitLimit
	}
	if c.LedgerLimitSize == 0 {
		c.LedgerLimitSize = DefaultLedgerLimitSize
	}
	if c.CloneAccounts == nil {
		c.CloneAccounts = []string{}
	}
	if c.CloneUpgradeablePrograms == nil {
		c.CloneUpgradeablePrograms = []string{}
	}
}

// Validate ensures configuration values are inside safe ranges.
func (c Config) Validate() error {
	if c.SlotsPerEpoch < minSlotsPerEpoch {
		return fmt.Errorf("slots_per_epoch must be >= %d", minSlotsPerEpoch)
	}
	if c.TicksPerSlot == 0 {
		return errors.New("ticks_per_slot must be >= 1")
	}
	if c.TicksPerSlot > maxTicksPerSlot {
		return fmt.Errorf("ticks_per_slot must be <= %d", maxTicksPerSlot)
	}
	if c.ComputeUnitLimit < minComputeUnitLimit {
		return fmt.Errorf("compute_unit_limit must be >= %d", minComputeUnitLimit)
	}
	if c.LedgerLimitSize < minLedgerLimitSize {
		return fmt.Errorf("ledger_limit_size must be >= %d", minLedgerLimitSize)
	}
	if err := validateAddressList("clone_accounts", c.CloneAccounts); err != nil {
		return err
	}
	if err := validateAddressList("clone_upgradeable_programs", c.CloneUpgradeablePrograms); err != nil {
		return err
	}
	if err := validateProgramDeploy(c.ProgramDeploy); err != nil {
		return err
	}
	return nil
}

func validateAddressList(key string, values []string) error {
	seen := map[string]struct{}{}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			return fmt.Errorf("%s contains an empty address", key)
		}
		if !base58AddressPattern.MatchString(value) {
			return fmt.Errorf("%s contains invalid address %q", key, value)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("%s contains duplicate address %q", key, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateProgramDeploy(cfg ProgramDeployConfig) error {
	soPath := strings.TrimSpace(cfg.SOPath)
	programIDKeypair := strings.TrimSpace(cfg.ProgramIDKeypairPath)
	upgradeAuthority := strings.TrimSpace(cfg.UpgradeAuthorityPath)

	if soPath == "" && programIDKeypair == "" && upgradeAuthority == "" {
		return nil
	}
	if soPath == "" {
		return errors.New("program_deploy.so_path is required when program_deploy is configured")
	}
	if !strings.HasSuffix(strings.ToLower(soPath), ".so") {
		return errors.New("program_deploy.so_path must point to a .so file")
	}
	if programIDKeypair == "" {
		return errors.New("program_deploy.program_id_keypair is required when program_deploy is configured")
	}
	if base58AddressPattern.MatchString(programIDKeypair) {
		return errors.New("program_deploy.program_id_keypair must be a keypair file path, not a pubkey")
	}
	if upgradeAuthority == "" {
		return errors.New("program_deploy.upgrade_authority is required when program_deploy is configured")
	}
	if base58AddressPattern.MatchString(upgradeAuthority) {
		return errors.New("program_deploy.upgrade_authority must be a keypair file path, not a pubkey")
	}
	return nil
}
