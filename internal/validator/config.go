package validator

// Config holds runtime validator parameters.
type Config struct {
	SlotsPerEpoch    uint64 `mapstructure:"slots_per_epoch" yaml:"slots_per_epoch"`
	ClockMultiplier  uint64 `mapstructure:"clock_multiplier" yaml:"clock_multiplier"`
	ComputeUnitLimit uint64 `mapstructure:"compute_unit_limit" yaml:"compute_unit_limit"`
}
