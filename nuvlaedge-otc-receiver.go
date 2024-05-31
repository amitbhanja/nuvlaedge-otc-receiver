package nuvlaedge_otc_receiver

import (
	"context"
	"encoding/json"
	"errors"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/receiver/receiverhelper"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net"
	"net/http"
	"sync"
)

const dataFormatProtobuf = "protobuf"

type nuvlaedgeConsumer struct {
	restrictedMetrics []string
	nextConsumer      consumer.Metrics
	settings          *receiver.CreateSettings
}

type nuvledgeOTCReceiver struct {
	cfg        *Config
	serverGRPC *grpc.Server
	serverHTTP *http.Server

	nextMetrics *nuvlaedgeConsumer
	shutdownWG  sync.WaitGroup

	obsrepGRPC *receiverhelper.ObsReport
	obsrepHTTP *receiverhelper.ObsReport

	settings *receiver.CreateSettings
}

type metricsReceiver struct {
	pmetricotlp.UnimplementedGRPCServer
	nextConsumer *nuvlaedgeConsumer
	obsreport    *receiverhelper.ObsReport
}

func (r *metricsReceiver) Export(ctx context.Context, req pmetricotlp.ExportRequest) (pmetricotlp.ExportResponse, error) {
	md := req.Metrics()
	dataPointCount := md.DataPointCount()
	if dataPointCount == 0 {
		return pmetricotlp.NewExportResponse(), nil
	}

	ctx = r.obsreport.StartMetricsOp(ctx)
	err := r.nextConsumer.ConsumeMetrics(ctx, md)
	r.obsreport.EndMetricsOp(ctx, dataFormatProtobuf, dataPointCount, err)

	// Use appropriate status codes for permanent/non-permanent errors
	// If we return the error straightaway, then the grpc implementation will set status code to Unknown
	// Refer: https://github.com/grpc/grpc-go/blob/v1.59.0/server.go#L1345
	// So, convert the error to appropriate grpc status and return the error
	// NonPermanent errors will be converted to codes.Unavailable (equivalent to HTTP 503)
	// Permanent errors will be converted to codes.InvalidArgument (equivalent to HTTP 400)
	if err != nil {
		return pmetricotlp.NewExportResponse(), GetStatusFromError(err)
	}

	return pmetricotlp.NewExportResponse(), nil
}

func GetStatusFromError(err error) error {
	s, ok := status.FromError(err)
	if !ok {
		// Default to a retryable error
		// https://github.com/open-telemetry/opentelemetry-proto/blob/main/docs/specification.md#failures
		code := codes.Unavailable
		if consumererror.IsPermanent(err) {
			// If an error is permanent but doesn't have an attached gRPC status, assume it is server-side.
			code = codes.Internal
		}
		s = status.New(code, err.Error())
	}
	return s.Err()
}

func newNuvlaedgeOTCReceiver(cfg *Config, set *receiver.CreateSettings, nextConsumer consumer.Metrics) (*nuvledgeOTCReceiver, error) {
	r := &nuvledgeOTCReceiver{
		cfg: cfg,
		nextMetrics: &nuvlaedgeConsumer{
			restrictedMetrics: cfg.RestrictedMetrics,
			nextConsumer:      nextConsumer,
			settings:          set,
		},
		settings: set,
	}

	var err error
	r.obsrepGRPC, err = receiverhelper.NewObsReport(receiverhelper.ObsReportSettings{
		ReceiverID:             set.ID,
		Transport:              "grpc",
		ReceiverCreateSettings: *set,
	})
	if err != nil {
		return nil, err
	}
	r.obsrepHTTP, err = receiverhelper.NewObsReport(receiverhelper.ObsReportSettings{
		ReceiverID:             set.ID,
		Transport:              "http",
		ReceiverCreateSettings: *set,
	})
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (r *nuvledgeOTCReceiver) startGRPCServer(host component.Host) error {
	if r.cfg.GRPC == nil {
		return nil
	}

	var err error
	if r.serverGRPC, err = r.cfg.GRPC.ToServer(context.Background(), host, r.settings.TelemetrySettings); err != nil {
		return err
	}

	if r.nextMetrics != nil {
		pmetricotlp.RegisterGRPCServer(r.serverGRPC, &metricsReceiver{
			nextConsumer: r.nextMetrics,
			obsreport:    r.obsrepGRPC,
		})
	}

	r.settings.Logger.Info("Starting GRPC server", zap.String("endpoint", r.cfg.GRPC.NetAddr.Endpoint))
	var gln net.Listener
	if gln, err = r.cfg.GRPC.NetAddr.Listen(context.Background()); err != nil {
		return err
	}

	r.shutdownWG.Add(1)
	go func() {
		defer r.shutdownWG.Done()
		if errGrpc := r.serverGRPC.Serve(gln); errGrpc != nil && !errors.Is(errGrpc, grpc.ErrServerStopped) {
			r.settings.Logger.Error("GRPC server error", zap.Error(err))
			r.settings.ReportStatus(component.NewFatalErrorEvent(errGrpc))
		}
	}()
	return nil

}

func (r *nuvledgeOTCReceiver) startHTTPServer(ctx context.Context, host component.Host) error {
	if r.cfg.HTTP == nil {
		return nil
	}
	httpMux := http.NewServeMux()
	if r.nextMetrics != nil {
		httpMetricsReceiver := &metricsReceiver{
			nextConsumer: r.nextMetrics,
			obsreport:    r.obsrepHTTP,
		}
		httpMux.HandleFunc(defaultMetricsURLPath, func(resp http.ResponseWriter, req *http.Request) {
			handleMetrics(resp, req, httpMetricsReceiver)
		})
	}

	var err error
	if r.serverHTTP, err = r.cfg.HTTP.ToServer(ctx, host, r.settings.TelemetrySettings, httpMux, confighttp.WithErrorHandler(errorHandler)); err != nil {
		return err
	}

	r.settings.Logger.Info("Starting HTTP server", zap.String("endpoint", r.cfg.HTTP.Endpoint))
	var hln net.Listener
	if hln, err = r.cfg.HTTP.ToListener(ctx); err != nil {
		return err
	}

	r.shutdownWG.Add(1)
	go func() {
		defer r.shutdownWG.Done()

		if errHTTP := r.serverHTTP.Serve(hln); errHTTP != nil && !errors.Is(errHTTP, http.ErrServerClosed) {
			r.settings.ReportStatus(component.NewFatalErrorEvent(errHTTP))
		}
	}()
	return nil
}

func (r *nuvledgeOTCReceiver) Start(ctx context.Context, host component.Host) error {
	if err := r.startGRPCServer(host); err != nil {
		return err
	}
	if err := r.startHTTPServer(ctx, host); err != nil {
		// It's possible that a valid GRPC server configuration was specified,
		// but an invalid HTTP configuration. If that's the case, the successfully
		// started GRPC server must be shutdown to ensure no goroutines are leaked.
		return errors.Join(err, r.Shutdown(ctx))
	}

	return nil
}

func (r *nuvledgeOTCReceiver) Shutdown(ctx context.Context) error {
	var err error

	if r.serverHTTP != nil {
		err = r.serverHTTP.Shutdown(ctx)
	}

	if r.serverGRPC != nil {
		r.serverGRPC.GracefulStop()
	}

	r.shutdownWG.Wait()
	return err
}
func getServiceName(md *pmetric.Metrics) string {
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		resource := rm.Resource()
		attrs := resource.Attributes()
		if serviceName, ok := attrs.Get("service.name"); ok {
			return serviceName.Str()
		}
	}
	return ""
}

func (r *nuvlaedgeConsumer) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	newMd := pmetric.NewMetrics()
	jsonData, err := json.Marshal(md)
	if err != nil {
		r.settings.Logger.Info("Error marshalling metrics: ", zap.String("error", err.Error()))
	} else {
		r.settings.Logger.Info("Received Metrics ", zap.String("metrics", string(jsonData)))
	}
	serviceName := getServiceName(&md)

	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		newRm := newMd.ResourceMetrics().AppendEmpty()
		rm.Resource().CopyTo(newRm.Resource())

		ilms := rm.ScopeMetrics()
		for j := 0; j < ilms.Len(); j++ {
			ilm := ilms.At(j)
			newIlm := newRm.ScopeMetrics().AppendEmpty()
			ilm.Scope().CopyTo(newIlm.Scope())
			ms := ilm.Metrics()

			for k := 0; k < ms.Len(); k++ {
				m := ms.At(k)
				forward := true
				r.settings.Logger.Info("Checking metric ", zap.String("name", m.Name()))
				for _, restrictedMetric := range r.restrictedMetrics {
					if serviceName != "" {
						restrictedMetric = serviceName + "_" + restrictedMetric
					}
					if m.Name() == restrictedMetric {
						forward = false
						break
					}
				}
				if forward {
					newM := newIlm.Metrics().AppendEmpty()
					r.settings.Logger.Info("Forwarding metric ", zap.String("name", m.Name()))
					m.CopyTo(newM)
				}
			}
		}
	}
	if r.nextConsumer != nil {
		return r.nextConsumer.ConsumeMetrics(ctx, newMd)
	}
	return nil
}
