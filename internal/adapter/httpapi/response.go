package httpapi

import (
	"io"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
)

// limitResponse also wraps early route and rate-limit replies. Without it,
// net/http may wait for an unread request body before flushing such a reply.
func (h *Handler) limitResponse(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		request := c.Request()
		var body *responseRequestBody
		if request.Body != nil && request.Body != http.NoBody {
			body = &responseRequestBody{ReadCloser: request.Body}
			request.Body = body
		}
		c.SetResponse(&boundedResponse{
			ResponseWriter: c.Response(),
			body:           body,
			protocol:       request.ProtoMajor,
			timeout:        h.opts.OperationTimeout,
		})
		return next(c)
	}
}

type responseRequestBody struct {
	io.ReadCloser
	eof bool
}

func (b *responseRequestBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.eof = true
	}
	return n, err
}

type boundedResponse struct {
	http.ResponseWriter
	body     *responseRequestBody
	protocol int
	timeout  time.Duration
	started  bool
}

func (w *boundedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *boundedResponse) WriteHeader(status int) {
	if !w.started {
		w.started = true
		controller := http.NewResponseController(w.ResponseWriter)
		now := time.Now()
		_ = controller.SetWriteDeadline(now.Add(w.timeout))
		if w.body != nil && !w.body.eof {
			// Rejected uploads and probe bodies are not needed. Prevent both
			// implicit draining and post-handler Close from waiting on the peer.
			if w.protocol == 1 {
				w.Header().Set("Connection", "close")
			}
			_ = controller.SetReadDeadline(now)
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *boundedResponse) Write(p []byte) (int, error) {
	if !w.started {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
