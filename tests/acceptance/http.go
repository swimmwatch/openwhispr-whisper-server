// Package acceptance contains shared acceptance workload and observation helpers.
// Helper tests are not evidence of CUDA operation or complete target acceptance.
package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FormField preserves order and duplicates so negative workloads reach the server.
// This helper deliberately does not validate or normalize transcription fields.
type FormField struct {
	Name, Value string
}

// PublicReply retains content only in memory for per-request assertions. Serialized
// observations exclude both the transcript and the server-provided error code.
// PublicDuration includes upload and response-body consumption, not request assembly.
// Queue time and backend execution cannot be inferred from this public duration.
type PublicReply struct {
	Status         int
	PublicDuration time.Duration
	Text           string `json:"-"`
	ErrorCode      string `json:"-"`
}

type PublicClient struct {
	endpoint      string
	client        *http.Client
	maxReplyBytes int64
}

// NewPublicClient owns a bounded, credential-free client with no redirect replay.
// endpoint is the API base URL, optionally including a reverse-proxy path prefix.
func NewPublicClient(endpoint string, timeout time.Duration, maxReplyBytes int64) (*PublicClient, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("an HTTP API base URL without credentials, query or fragment is required")
	}
	if timeout <= 0 || maxReplyBytes <= 0 || maxReplyBytes == math.MaxInt64 {
		return nil, errors.New("positive bounded request timeout and reply size are required")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/audio/transcriptions"
	u.RawPath = ""
	return &PublicClient{
		endpoint: u.String(),
		client: &http.Client{
			Timeout:   timeout,
			Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxReplyBytes: maxReplyBytes,
	}, nil
}

// Close releases this workload client's idle connections, not another client's pool.
func (c *PublicClient) Close() {
	c.client.CloseIdleConnections()
}

// Transcribe sends exactly one multipart request. It does not retry inference.
// audio is an already bounded public fixture owned by the caller; no goroutine
// reads an arbitrary blocking source or outlives request cancellation.
func (c *PublicClient) Transcribe(ctx context.Context, audio []byte, filename string, fields []FormField) (PublicReply, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, field := range fields {
		if err := form.WriteField(field.Name, field.Value); err != nil {
			return PublicReply{}, errors.New("cannot assemble transcription fields")
		}
	}
	part, err := form.CreateFormFile("file", filename)
	if err != nil {
		return PublicReply{}, errors.New("cannot assemble transcription audio")
	}
	if _, err := part.Write(audio); err != nil {
		return PublicReply{}, errors.New("cannot assemble transcription audio")
	}
	if err := form.Close(); err != nil {
		return PublicReply{}, errors.New("cannot finish transcription multipart body")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, &body)
	if err != nil {
		return PublicReply{}, errors.New("cannot construct public transcription request")
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	started := time.Now()
	response, err := c.client.Do(request)
	if err != nil {
		return PublicReply{PublicDuration: time.Since(started)}, &publicRequestError{err}
	}
	defer response.Body.Close()
	reply := PublicReply{Status: response.StatusCode}
	content, err := io.ReadAll(io.LimitReader(response.Body, c.maxReplyBytes+1))
	reply.PublicDuration = time.Since(started)
	if err != nil {
		return reply, &publicRequestError{err}
	}
	if int64(len(content)) > c.maxReplyBytes {
		return reply, errors.New("public response exceeded the workload reply bound")
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		return reply, errors.New("public response has an invalid content type")
	}
	if response.StatusCode == http.StatusOK && mediaType == "text/plain" {
		reply.Text = string(content)
		return reply, nil
	}
	if mediaType != "application/json" {
		return reply, errors.New("public response has an unsupported content type")
	}
	if response.StatusCode == http.StatusOK {
		var result struct {
			Text *string `json:"text"`
		}
		if decodePublicJSON(content, &result) != nil || result.Text == nil {
			return reply, errors.New("public transcription response is invalid")
		}
		reply.Text = *result.Text
		return reply, nil
	}
	var failure struct {
		Error *struct {
			Message string  `json:"message"`
			Type    string  `json:"type"`
			Param   *string `json:"param"`
			Code    string  `json:"code"`
		} `json:"error"`
	}
	if decodePublicJSON(content, &failure) != nil || failure.Error == nil || failure.Error.Code == "" {
		return reply, errors.New("public error response is invalid")
	}
	reply.ErrorCode = failure.Error.Code
	return reply, nil
}

func decodePublicJSON(content []byte, result any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("trailing public response data")
	}
	return nil
}

// Transport errors can contain private endpoints. Preserve errors.Is/As and the
// cancellation cause without including their raw diagnostics in this error text.
type publicRequestError struct{ cause error }

func (e *publicRequestError) Error() string { return "public transcription request failed" }
func (e *publicRequestError) Unwrap() error { return e.cause }
