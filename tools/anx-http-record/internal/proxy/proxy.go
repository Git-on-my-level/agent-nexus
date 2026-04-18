package proxy

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"agent-nexus-tools-anx-http-record/internal/recorder"
)

type Options struct {
	Upstream      *url.URL
	Recorder      *recorder.JSONLWriter
	MaxBodyBytes  int64
	MutationsOnly bool
	Logger        *log.Logger
}

func NewHandler(opts Options) (http.Handler, error) {
	if opts.Upstream == nil {
		return nil, fmt.Errorf("upstream is required")
	}
	if opts.Recorder == nil {
		return nil, fmt.Errorf("recorder is required")
	}
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			state := newExchangeState(pr.In, opts.MaxBodyBytes, opts.MutationsOnly)
			pr.Out = pr.Out.WithContext(context.WithValue(pr.Out.Context(), exchangeStateKey{}, state))
			pr.SetURL(opts.Upstream)
			pr.SetXForwarded()
			pr.Out.Header.Del("X-ANX-Record-Agent")
			if !state.skip && pr.Out.Body != nil {
				pr.Out.Body = recorder.NewCaptureReadCloser(pr.Out.Body, state.requestCapture, nil)
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			state := exchangeStateFromContext(resp.Request.Context())
			if state == nil || state.skip {
				return nil
			}
			state.statusCode = resp.StatusCode
			state.responseHeader = resp.Header.Clone()

			captureBody := true
			if isExcludedStreamingResponse(resp) {
				state.responseBodyOmitted = true
				captureBody = false
			}

			resp.Body = recorder.NewCaptureReadCloser(resp.Body, bodySink(captureBody, state.responseCapture), func() {
				state.finish(opts, "")
			})
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			state := exchangeStateFromContext(req.Context())
			if state != nil && !state.skip {
				state.statusCode = http.StatusBadGateway
				state.finish(opts, err.Error())
			}
			http.Error(w, "upstream request failed", http.StatusBadGateway)
		},
	}
	return proxy, nil
}

func bodySink(enabled bool, sink *recorder.LimitedBuffer) *recorder.LimitedBuffer {
	if !enabled {
		return nil
	}
	return sink
}

type exchangeStateKey struct{}

type exchangeState struct {
	start               time.Time
	method              string
	path                string
	query               string
	clientLabel         string
	requestHeader       http.Header
	responseHeader      http.Header
	requestCapture      *recorder.LimitedBuffer
	responseCapture     *recorder.LimitedBuffer
	requestBodyOmitted  bool
	responseBodyOmitted bool
	statusCode          int
	skip                bool

	finishOnce sync.Once
}

func newExchangeState(req *http.Request, maxBodyBytes int64, mutationsOnly bool) *exchangeState {
	label := strings.TrimSpace(req.Header.Get("X-ANX-Record-Agent"))
	if label == "" {
		label = strings.TrimSpace(req.Header.Get("X-ANX-Agent"))
	}
	path := req.URL.Path
	return &exchangeState{
		start:           time.Now(),
		method:          req.Method,
		path:            path,
		query:           req.URL.RawQuery,
		clientLabel:     label,
		requestHeader:   req.Header.Clone(),
		requestCapture:  recorder.NewLimitedBuffer(maxBodyBytes),
		responseCapture: recorder.NewLimitedBuffer(maxBodyBytes),
		skip:            shouldSkipRecording(req.Method, path, mutationsOnly),
	}
}

func exchangeStateFromContext(ctx context.Context) *exchangeState {
	state, _ := ctx.Value(exchangeStateKey{}).(*exchangeState)
	return state
}

func shouldSkipRecording(method string, path string, mutationsOnly bool) bool {
	switch path {
	case "/readyz", "/version", "/events/stream", "/inbox/stream":
		return true
	}
	if !mutationsOnly {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return false
	default:
		return true
	}
}

func isExcludedStreamingResponse(resp *http.Response) bool {
	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	return strings.HasPrefix(contentType, "text/event-stream")
}

func (s *exchangeState) finish(opts Options, recordErr string) {
	s.finishOnce.Do(func() {
		requestBody := recorder.MaterializeBody(s.requestHeader, s.requestCapture.Bytes(), s.requestCapture.Truncated())
		responseBody := recorder.MaterializeBody(s.responseHeader, s.responseCapture.Bytes(), s.responseCapture.Truncated())

		entry := recorder.Entry{
			Method:                s.method,
			Path:                  s.path,
			Query:                 s.query,
			RequestHeaders:        recorder.RedactHeaders(s.requestHeader),
			ResponseHeaders:       recorder.RedactHeaders(s.responseHeader),
			RequestBody:           requestBody.Value,
			RequestBodyEncoding:   requestBody.Encoding,
			ResponseBody:          responseBody.Value,
			ResponseBodyEncoding:  responseBody.Encoding,
			StatusCode:            s.statusCode,
			TruncatedRequestBody:  s.requestCapture.Truncated(),
			TruncatedResponseBody: s.responseCapture.Truncated(),
			RequestBodyOmitted:    s.requestBodyOmitted || requestBody.Omitted,
			ResponseBodyOmitted:   s.responseBodyOmitted || responseBody.Omitted,
			RequestBodyRedacted:   requestBody.Redacted,
			ResponseBodyRedacted:  responseBody.Redacted,
			ClientLabel:           s.clientLabel,
			DurationMS:            time.Since(s.start).Milliseconds(),
			Error:                 recordErr,
		}
		if err := opts.Recorder.Write(entry); err != nil {
			opts.Logger.Printf("write recording entry: %v", err)
		}
	})
}
