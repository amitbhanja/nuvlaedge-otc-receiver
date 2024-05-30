package nuvlaedge_otc_receiver

import (
	"bytes"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	spb "google.golang.org/genproto/googleapis/rpc/status"
)

const (
	pbContentType   = "application/x-protobuf"
	jsonContentType = "application/json"
)

var (
	pbEncoder       = &protoEncoder{}
	jsEncoder       = &jsonEncoder{}
	jsonPbMarshaler = &jsonpb.Marshaler{}
)

type encoder interface {
	unmarshalMetricsRequest(buf []byte) (pmetricotlp.ExportRequest, error)
	marshalMetricsResponse(pmetricotlp.ExportResponse) ([]byte, error)
	contentType() string
	marshalStatus(rsp *spb.Status) ([]byte, error)
}

type protoEncoder struct{}

func (protoEncoder) unmarshalMetricsRequest(buf []byte) (pmetricotlp.ExportRequest, error) {
	req := pmetricotlp.NewExportRequest()
	err := req.UnmarshalProto(buf)
	return req, err
}

func (protoEncoder) marshalMetricsResponse(resp pmetricotlp.ExportResponse) ([]byte, error) {
	return resp.MarshalProto()
}

func (protoEncoder) contentType() string {
	return pbContentType
}

func (protoEncoder) marshalStatus(rsp *spb.Status) ([]byte, error) {
	return proto.Marshal(rsp)
}

type jsonEncoder struct{}

func (jsonEncoder) unmarshalMetricsRequest(buf []byte) (pmetricotlp.ExportRequest, error) {
	req := pmetricotlp.NewExportRequest()
	err := req.UnmarshalJSON(buf)
	return req, err
}

func (jsonEncoder) marshalMetricsResponse(resp pmetricotlp.ExportResponse) ([]byte, error) {
	return resp.MarshalJSON()
}

func (jsonEncoder) contentType() string {
	return jsonContentType
}

func (jsonEncoder) marshalStatus(rsp *spb.Status) ([]byte, error) {
	buf := new(bytes.Buffer)
	err := jsonPbMarshaler.Marshal(buf, rsp)
	return buf.Bytes(), err
}
