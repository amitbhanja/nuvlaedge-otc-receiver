package nuvlaedge_otc_receiver

import (
	"context"
	"fmt"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configgrpc"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/config/confignet"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
)

var (
	typeStr = component.MustNewType("nuvlaedge_otc_receiver")
)

const (
	grpcPort = 4317
	httpPort = 4318
)

func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		typeStr,
		createDefaultConfig,
		receiver.WithMetrics(createMetrics, component.StabilityLevelBeta),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		Protocols: Protocols{
			GRPC: &configgrpc.ServerConfig{
				NetAddr: confignet.AddrConfig{
					Endpoint:  fmt.Sprintf("%s:%d", "0.0.0.0", grpcPort),
					Transport: confignet.TransportTypeTCP,
				},
				ReadBufferSize: 512 * 1024,
			},
			HTTP: &confighttp.ServerConfig{
				Endpoint: fmt.Sprintf("%s:%d", "0.0.0.0", httpPort),
			},
		},
		RestrictedMetrics: []string{},
	}
}

func createMetrics(
	_ context.Context,
	set receiver.CreateSettings,
	cfg component.Config,
	consumer consumer.Metrics,
) (receiver.Metrics, error) {
	fmt.Printf("createMetrics\n")
	return newNuvlaedgeOTCReceiver(
		cfg.(*Config),
		&set,
		consumer,
	)
}
