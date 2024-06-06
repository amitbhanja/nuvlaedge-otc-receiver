package nuvlaedge_otc_receiver

import (
	"fmt"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"mime"
	"net/http"
)

var fallbackMsg = []byte(`{"code": 13, "message": "failed to marshal error message"}`)

const fallbackContentType = "application/json"

func readContentType(resp http.ResponseWriter, req *http.Request) (encoder, bool) {
	if req.Method != http.MethodPost {
		handleUnmatchedMethod(resp)
		return nil, false
	}

	switch getMimeTypeFromContentType(req.Header.Get("Content-Type")) {
	case pbContentType:
		return pbEncoder, true
	case jsonContentType:
		return jsEncoder, true
	default:
		handleUnmatchedContentType(resp)
		return nil, false
	}
}
func handleMetrics(resp http.ResponseWriter, req *http.Request, metricsReceiver *metricsReceiver) {
	enc, ok := readContentType(resp, req)
	if !ok {
		return
	}

	body, ok := readAndCloseBody(resp, req, enc)
	if !ok {
		return
	}

	otlpReq, err := enc.unmarshalMetricsRequest(body)
	if err != nil {
		writeError(resp, enc, err, http.StatusBadRequest)
		return
	}

	otlpResp, err := metricsReceiver.Export(req.Context(), otlpReq)
	if err != nil {
		writeError(resp, enc, err, http.StatusInternalServerError)
		return
	}

	msg, err := enc.marshalMetricsResponse(otlpResp)
	if err != nil {
		writeError(resp, enc, err, http.StatusInternalServerError)
		return
	}
	writeResponse(resp, enc.contentType(), http.StatusOK, msg)
}

func readAndCloseBody(resp http.ResponseWriter, req *http.Request, enc encoder) ([]byte, bool) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		writeError(resp, enc, err, http.StatusBadRequest)
		return nil, false
	}
	if err = req.Body.Close(); err != nil {
		writeError(resp, enc, err, http.StatusBadRequest)
		return nil, false
	}
	return body, true
}

// writeError encodes the HTTP error inside a rpc.Status message as required by the OTLP protocol.
func writeError(w http.ResponseWriter, encoder encoder, err error, statusCode int) {
	s, ok := status.FromError(err)
	if ok {
		statusCode = GetHTTPStatusCodeFromStatus(s)
	} else {
		s = NewStatusFromMsgAndHTTPCode(err.Error(), statusCode)
	}
	writeStatusResponse(w, encoder, statusCode, s.Proto())
}

// errorHandler encodes the HTTP error message inside a rpc.Status message as required
// by the OTLP protocol.
func errorHandler(w http.ResponseWriter, r *http.Request, errMsg string, statusCode int) {
	s := NewStatusFromMsgAndHTTPCode(errMsg, statusCode)
	switch getMimeTypeFromContentType(r.Header.Get("Content-Type")) {
	case pbContentType:
		writeStatusResponse(w, pbEncoder, statusCode, s.Proto())
		return
	case jsonContentType:
		writeStatusResponse(w, jsEncoder, statusCode, s.Proto())
		return
	}
	writeResponse(w, fallbackContentType, http.StatusInternalServerError, fallbackMsg)
}

func writeStatusResponse(w http.ResponseWriter, enc encoder, statusCode int, rsp *spb.Status) {
	msg, err := enc.marshalStatus(rsp)
	if err != nil {
		writeResponse(w, fallbackContentType, http.StatusInternalServerError, fallbackMsg)
		return
	}

	writeResponse(w, enc.contentType(), statusCode, msg)
}

func writeResponse(w http.ResponseWriter, contentType string, statusCode int, msg []byte) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(statusCode)
	// Nothing we can do with the error if we cannot write to the response.
	_, _ = w.Write(msg)
}

func getMimeTypeFromContentType(contentType string) string {
	mediatype, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return mediatype
}

func handleUnmatchedMethod(resp http.ResponseWriter) {
	status := http.StatusMethodNotAllowed
	writeResponse(resp, "text/plain", status, []byte(fmt.Sprintf("%v method not allowed, supported: [POST]", status)))
}

func handleUnmatchedContentType(resp http.ResponseWriter) {
	status := http.StatusUnsupportedMediaType
	writeResponse(resp, "text/plain", status, []byte(fmt.Sprintf("%v unsupported media type, supported: [%s, %s]", status, jsonContentType, pbContentType)))
}

func GetHTTPStatusCodeFromStatus(s *status.Status) int {
	switch s.Code() {
	case codes.Canceled, codes.DeadlineExceeded, codes.Aborted, codes.OutOfRange, codes.Unavailable, codes.DataLoss:
		return http.StatusServiceUnavailable
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

func NewStatusFromMsgAndHTTPCode(errMsg string, statusCode int) *status.Status {
	var c codes.Code
	switch statusCode {
	case http.StatusBadRequest:
		c = codes.InvalidArgument
	case http.StatusUnauthorized:
		c = codes.Unauthenticated
	case http.StatusForbidden:
		c = codes.PermissionDenied
	case http.StatusNotFound:
		c = codes.Unimplemented
	case http.StatusTooManyRequests:
		c = codes.ResourceExhausted
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		c = codes.Unavailable
	default:
		c = codes.Unknown
	}
	return status.New(c, errMsg)
}
