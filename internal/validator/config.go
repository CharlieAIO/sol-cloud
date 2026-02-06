package validator

import (
	"errors"
	"fmt"
)

const (
	DefaultSlotsPerEpoch    uint64 = 432000
	DefaultClockMultiplier  uint64 = 1
	DefaultComputeUnitLimit uint64 = 200000

	minSlotsPerEpoch    uint64 = 32
	maxClockMultiplier  uint64 = 1000
	minComputeUnitLimit uint64 = 10000
)

// Config holds runtime validator parameters.
type Config struct {
	SlotsPerEpoch    uint64 `mapstructure:"slots_per_epoch" yaml:"slots_per_epoch"`
	ClockMultiplier  uint64 `mapstructure:"clock_multiplier" yaml:"clock_multiplier"`
	ComputeUnitLimit uint64 `mapstructure:"compute_unit_limit" yaml:"compute_unit_limit"`
}

// DefaultConfig returns the baseline validator configuration.
func DefaultConfig() Config {
	return Config{
		SlotsPerEpoch:    DefaultSlotsPerEpoch,
		ClockMultiplier:  DefaultClockMultiplier,
		ComputeUnitLimit: DefaultComputeUnitLimit,
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
	if c.ClockMultiplier == 0 {
		c.ClockMultiplier = DefaultClockMultiplier
	}
	if c.ComputeUnitLimit == 0 {
		c.ComputeUnitLimit = DefaultComputeUnitLimit
	}
}

// Validate ensures configuration values are inside safe ranges.
func (c Config) Validate() error {
	if c.SlotsPerEpoch < minSlotsPerEpoch {
		return fmt.Errorf("slots_per_epoch must be >= %d", minSlotsPerEpoch)
	}
	if c.ClockMultiplier == 0 {
		return errors.New("clock_multiplier must be >= 1")
	}
	if c.ClockMultiplier > maxClockMultiplier {
		return fmt.Errorf("clock_multiplier must be <= %d", maxClockMultiplier)
	}
	if c.ComputeUnitLimit < minComputeUnitLimit {
		return fmt.Errorf("compute_unit_limit must be >= %d", minComputeUnitLimit)
	}
	return nil
}

func (c Config) TicksPerSlot() uint64 {
	const baseTicks uint64 = 64
	if c.ClockMultiplier <= 1 {
		return baseTicks
	}
	ticks := baseTicks / c.ClockMultiplier
	if ticks == 0 {
		return 1
	}
	return ticks
}
