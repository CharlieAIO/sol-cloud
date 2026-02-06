package config

import "github.com/CharlieAIO/sol-cloud/internal/validator"

// AppConfig is the top-level .sol-cloud.yml model.
type AppConfig struct {
	Provider  string           `mapstructure:"provider" yaml:"provider"`
	AppName   string           `mapstructure:"app_name" yaml:"app_name"`
	Region    string           `mapstructure:"region" yaml:"region"`
	Validator validator.Config `mapstructure:"validator" yaml:"validator"`
}
