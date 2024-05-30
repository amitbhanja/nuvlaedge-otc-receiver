package nuvlaedge_otc_receiver

import (
	"errors"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configgrpc"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/confmap"
)

const (
	// Protocol values.
	protoGRPC             = "protocols::grpc"
	protoHTTP             = "protocols::http"
	defaultMetricsURLPath = "/v1/metrics"
)

type Protocols struct {
	GRPC *configgrpc.ServerConfig `mapstructure:"grpc"`
	HTTP *confighttp.ServerConfig `mapstructure:"http"`
}

type Config struct {
	Protocols         `mapstructure:"protocols"`
	RestrictedMetrics []string `mapstructure:"restricted_metrics, omitempty"`
}

func (cfg *Config) Validate() error {
	if cfg.GRPC == nil && cfg.HTTP == nil {
		return errors.New("must specify at least one protocol when using the OTLP receiver")
	}
	return nil
}

func (cfg *Config) Unmarshal(conf *confmap.Conf) error {
	// first load the config normally
	err := conf.Unmarshal(cfg)
	if err != nil {
		return err
	}

	if !conf.IsSet(protoGRPC) {
		cfg.GRPC = nil
	}

	if !conf.IsSet(protoHTTP) {
		cfg.HTTP = nil
	}

	return nil
}

var _ component.Config = (*Config)(nil)
var _ confmap.Unmarshaler = (*Config)(nil)
