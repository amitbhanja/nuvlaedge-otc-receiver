package nuvlaedge_otc_receiver

import (
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
)

type Config struct {
	OTLPConfig        otlpreceiver.Config `mapstructure:"otlp"`
	RestrictedMetrics []string            `mapstructure:"restricted_metrics"`
}

func (cfg *Config) Validate() error {
	return cfg.OTLPConfig.Validate()
}

func (cfg *Config) Unmarshal(conf *confmap.Conf) error {
	return cfg.OTLPConfig.Unmarshal(conf)
}

var _ component.Config = (*Config)(nil)
var _ confmap.Unmarshaler = (*Config)(nil)
