package acceptance

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicClientRejectsInvalidUTF8Replies(t *testing.T) {
	for _, media := range []string{"application/json", "text/plain"} {
		t.Run(media, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", media)
				body := "speech\xff"
				if media == "application/json" {
					body = `{"text":"` + body + `"}`
				}
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			reply, err := newPublicClient(t, server.URL, 4096).Transcribe(t.Context(), nil, "public.wav", nil)
			if err == nil || reply.Text != "" || reply.ErrorCode != "" {
				t.Fatal("invalid UTF-8 was accepted or normalized into a successful observation")
			}
		})
	}
}

func TestPublicErrorEnvelopeValidationAcrossRequestPaths(t *testing.T) {
	for _, tc := range []struct {
		name, media, body string
		valid             bool
	}{
		{"complete", "application/json", `{"error":{"message":"safe","type":"invalid_request_error","param":null,"code":"invalid_request"}}`, true},
		{"field-param", "application/json", `{"error":{"message":"safe","type":"invalid_request_error","param":"file","code":"invalid_request"}}`, true},
		{"missing-message", "application/json", `{"error":{"type":"invalid_request_error","param":null,"code":"invalid_request"}}`, false},
		{"null-message", "application/json", `{"error":{"message":null,"type":"invalid_request_error","param":null,"code":"invalid_request"}}`, false},
		{"missing-type", "application/json", `{"error":{"message":"safe","param":null,"code":"invalid_request"}}`, false},
		{"invalid-type", "application/json", `{"error":{"message":"safe","type":"PRIVATE_RESPONSE","param":null,"code":"invalid_request"}}`, false},
		{"missing-param", "application/json", `{"error":{"message":"safe","type":"invalid_request_error","code":"invalid_request"}}`, false},
		{"invalid-param", "application/json", `{"error":{"message":"safe","type":"invalid_request_error","param":"PRIVATE_RESPONSE","code":"invalid_request"}}`, false},
		{"extra-field", "application/json", `{"error":{"message":"safe","type":"invalid_request_error","param":null,"code":"invalid_request","cause":"PRIVATE_RESPONSE"}}`, false},
		{"wrong-media", "text/plain", `{"error":{"message":"safe","type":"invalid_request_error","param":null,"code":"invalid_request"}}`, false},
		{"overlong-message", "application/json", `{"error":{"message":"` + strings.Repeat("x", 257) + `","type":"invalid_request_error","param":null,"code":"invalid_request"}}`, false},
		{"unknown-code", "application/json", `{"error":{"message":"safe","type":"invalid_request_error","param":null,"code":"PRIVATE_RESPONSE"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", tc.media)
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client := newPublicClient(t, server.URL, 4096)
			for _, raw := range []bool{false, true} {
				var reply PublicReply
				var err error
				if raw {
					reply, err = sendPublicCaseRaw(t.Context(), client, []byte("invalid multipart"), "multipart/form-data")
				} else {
					reply, err = client.Transcribe(t.Context(), nil, "public.wav", nil)
				}
				if (err == nil) != tc.valid {
					t.Errorf("raw=%t valid=%t response validation mismatch", raw, tc.valid)
				}
				if !tc.valid && (reply.ErrorCode != "" || reply.Text != "" || err != nil && strings.Contains(err.Error(), "PRIVATE_RESPONSE")) {
					t.Errorf("raw=%t invalid response content escaped validation", raw)
				}
				if tc.valid && checkPublicCase(reply, err, http.StatusBadRequest, "invalid_request") != nil {
					t.Errorf("raw=%t complete canonical error rejected", raw)
				}
			}
		})
	}
}
