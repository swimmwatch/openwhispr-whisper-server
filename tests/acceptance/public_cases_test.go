package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"
)

// Only allowlisted recognition facts may leave the in-memory response. These
// observations diagnose ASR variation; they contain no original text or tokens.
type publicSpeechObservation struct {
	Bytes, Runes                                                                        int
	SHA256                                                                              string
	Ask, Not, What, Your, You, Fellow, Americans, American, Country, Countries, Can, Do bool
}

func observePublicSpeech(text string) publicSpeechObservation {
	words := make(map[string]bool)
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) }) {
		switch word {
		case "ask", "not", "what", "your", "you", "fellow", "americans", "american", "country", "countries", "can", "do":
			words[word] = true
		}
	}
	digest := sha256.Sum256([]byte(text))
	return publicSpeechObservation{
		Bytes: len(text), Runes: utf8.RuneCountInString(text), SHA256: hex.EncodeToString(digest[:]),
		Ask: words["ask"], Not: words["not"], What: words["what"], Your: words["your"], You: words["you"], Fellow: words["fellow"], Americans: words["americans"], American: words["american"], Country: words["country"], Countries: words["countries"], Can: words["can"], Do: words["do"],
	}
}

const publicFixtureLimit int64 = 25 << 20
const publicJFKIdentity = "59dfb9a4acb36fe2a2affc14bacbee2920ff435cb13cc314a08c13f66ba7860e"

// runPublicCases owns this client sequentially for the duration of the slice.
// No retry, readiness wait or pacing hides a failure between successful requests.
// These public outcomes do not prove rejection before native admission; the parent
// must independently correlate its native generation/activity observations.
func runPublicCases(t *testing.T, ctx context.Context, client *PublicClient, fixturesDir, model string) {
	t.Helper()
	wire := &publicCaseWire{next: client.client.Transport}
	if wire.next == nil {
		wire.next = http.DefaultTransport
	}
	original := client.client.Transport
	client.client.Transport = wire
	defer func() { client.client.Transport = original }()
	fields := []FormField{{"model", model}, {"language", "en"}, {"temperature", "0"}}
	for _, name := range []string{"jfk.wav", "jfk.mp3", "jfk.mpeg", "jfk.mpga", "jfk.mp4", "jfk.m4a", "jfk.ogg", "jfk-opus.ogg", "jfk.webm", "jfk.flac"} {
		t.Run("format/"+name, func(t *testing.T) {
			audio, err := readPublicCaseFixture(fixturesDir, name)
			if err != nil {
				t.Fatal(err)
			}
			before := wire.requests.Load()
			reply, err := client.Transcribe(ctx, audio, name, fields)
			t.Logf("public speech facts: status=%d transport_error=%t media=%s requests=%d observation=%+v", reply.Status, err != nil, publicCaseMediaLabel(wire.mediaType()), wire.requests.Load()-before, observePublicSpeech(reply.Text))
			if checkPublicCase(reply, err, http.StatusOK, "") != nil || wire.requests.Load()-before != 1 || wire.mediaType() != "application/json" {
				t.Fatalf("identified format failed: status=%d transport_error=%t requests=%d", reply.Status, err != nil, wire.requests.Load()-before)
			}
		})
	}
	wav, err := readPublicCaseFixture(fixturesDir, "jfk.wav")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, filename, media string
		fields                []FormField
		recognizeSpeech       bool
	}{
		{"defaults", "jfk.wav", "application/json", []FormField{{"model", model}}, true},
		{"json", "jfk.wav", "application/json", appendFields(fields, FormField{"response_format", "json"}), true},
		{"text", "jfk.wav", "text/plain", appendFields(fields, FormField{"response_format", "text"}), true},
		{"explicit-empty-prompt-and-stream-false", "jfk.wav", "application/json", appendFields(fields, FormField{"prompt", ""}, FormField{"stream", "false"}), true},
		{"auto-language", "jfk.wav", "application/json", []FormField{{"model", model}, {"language", "auto"}, {"temperature", "0"}}, true},
		// This option tests REQ-012's numeric boundary, not lexical ASR quality.
		// Actual high-temperature output can be nonempty without fixture cues;
		// only this case omits phrase recognition, never protocol/body checks.
		{"temperature-upper-bound", "jfk.wav", "application/json", []FormField{{"model", model}, {"language", "en"}, {"temperature", "1"}}, false},
		{"content-not-extension", "public-fixture.bin", "application/json", fields, true},
	} {
		t.Run("options/"+tc.name, func(t *testing.T) {
			before := wire.requests.Load()
			reply, err := client.Transcribe(ctx, wav, tc.filename, tc.fields)
			t.Logf("public speech facts: status=%d transport_error=%t media=%s requests=%d observation=%+v", reply.Status, err != nil, publicCaseMediaLabel(wire.mediaType()), wire.requests.Load()-before, observePublicSpeech(reply.Text))
			if checkPublicOption(reply, err, tc.media, wire.mediaType(), wire.requests.Load()-before, tc.recognizeSpeech) != nil {
				t.Fatalf("supported options failed transcript/representation: status=%d transport_error=%t nonempty=%t country_marker=%t media=%s requests=%d", reply.Status, err != nil, strings.TrimSpace(reply.Text) != "", strings.Contains(strings.ToLower(reply.Text), "country"), publicCaseMediaLabel(wire.mediaType()), wire.requests.Load()-before)
			}
		})
	}
	type rejectedCase struct {
		name   string
		fields []FormField
		audio  []byte
		status int
		code   string
	}
	invalid := []rejectedCase{
		{"missing-model", nil, wav, http.StatusBadRequest, "invalid_request"},
		{"empty-model", []FormField{{"model", ""}}, wav, http.StatusBadRequest, "invalid_request"},
		{"invalid-model-path", []FormField{{"model", "../../base"}}, wav, http.StatusBadRequest, "invalid_request"},
		{"model-id-too-long", []FormField{{"model", strings.Repeat("a", 129)}}, wav, http.StatusBadRequest, "invalid_request"},
		{"unknown-model", []FormField{{"model", model + "-p07-not-served"}}, wav, http.StatusNotFound, "model_not_found"},
		{"duplicate-model", appendFields(fields, FormField{"model", model}), wav, http.StatusBadRequest, "invalid_request"},
		{"unknown-field", appendFields(fields, FormField{"p07_unsupported", "value"}), wav, http.StatusBadRequest, "invalid_request"},
		{"invalid-language", []FormField{{"model", model}, {"language", "p07-invalid-language"}}, wav, http.StatusBadRequest, "invalid_request"},
		{"invalid-utf8-prompt", appendFields(fields, FormField{"prompt", string([]byte{0xff})}), wav, http.StatusBadRequest, "invalid_request"},
		{"empty-file", fields, nil, http.StatusBadRequest, "invalid_request"},
		{"corrupt-content", fields, []byte("P07_CORRUPT_PUBLIC_AUDIO_NOT_A_RECORDING"), http.StatusBadRequest, "unsupported_audio"},
	}
	for _, value := range []string{"-0.01", "1.01", "NaN", "+Inf", "-Inf", "", "not-a-number"} {
		invalid = append(invalid, rejectedCase{"temperature-" + value, []FormField{{"model", model}, {"temperature", value}}, wav, http.StatusBadRequest, "invalid_request"})
	}
	for _, field := range []FormField{{"language", "en"}, {"prompt", ""}, {"temperature", "0"}, {"response_format", "json"}, {"stream", "false"}} {
		invalid = append(invalid, rejectedCase{"duplicate-" + field.Name, []FormField{{"model", model}, field, field}, wav, http.StatusBadRequest, "invalid_request"})
	}
	for _, field := range []FormField{{"stream", "true"}, {"stream", "False"}, {"response_format", "verbose_json"}, {"response_format", ""}} {
		invalid = append(invalid, rejectedCase{field.Name + "-" + field.Value, appendFields(fields, field), wav, http.StatusBadRequest, "invalid_request"})
	}
	for _, tc := range invalid {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			before := wire.requests.Load()
			reply, err := client.Transcribe(ctx, tc.audio, "jfk.wav", tc.fields)
			if checkPublicCase(reply, err, tc.status, tc.code) != nil || wire.requests.Load()-before != 1 {
				t.Fatalf("server-side rejection mismatch: status=%d expected_status=%d code_matches=%t transport_error=%t requests=%d", reply.Status, tc.status, reply.ErrorCode == tc.code, err != nil, wire.requests.Load()-before)
			}
		})
	}
	for _, tc := range []struct {
		name         string
		files        int
		includeModel bool
	}{{"missing-file", 0, true}, {"duplicate-file", 2, true}, {"empty-multipart", 0, false}} {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			if tc.includeModel {
				if err := form.WriteField("model", model); err != nil {
					t.Fatal("cannot prepare negative multipart field")
				}
			}
			for i := 0; i < tc.files; i++ {
				part, err := form.CreateFormFile("file", "jfk.wav")
				if err != nil {
					t.Fatal("cannot prepare negative multipart file")
				}
				if _, err := part.Write(wav); err != nil {
					t.Fatal("cannot prepare negative audio bytes")
				}
			}
			if err := form.Close(); err != nil {
				t.Fatal("cannot complete negative multipart framing")
			}
			reply, err := sendPublicCaseRaw(ctx, client, body.Bytes(), form.FormDataContentType())
			if checkPublicCase(reply, err, http.StatusBadRequest, "invalid_request") != nil {
				t.Fatalf("multipart rejection mismatch: status=%d code_matches=%t transport_error=%t", reply.Status, reply.ErrorCode == "invalid_request", err != nil)
			}
		})
	}
	for _, tc := range []struct{ name, contentType, body string }{{"truncated-multipart", "multipart/form-data; boundary=p07-boundary", "--p07-boundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\n" + model}, {"missing-boundary", "multipart/form-data", "not multipart"}, {"wrong-content-type", "application/json", `{}`}, {"empty-body", "multipart/form-data; boundary=p07-boundary", ""}} {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			reply, err := sendPublicCaseRaw(ctx, client, []byte(tc.body), tc.contentType)
			if checkPublicCase(reply, err, http.StatusBadRequest, "invalid_request") != nil {
				t.Fatalf("framing rejection mismatch: status=%d code_matches=%t transport_error=%t", reply.Status, reply.ErrorCode == "invalid_request", err != nil)
			}
		})
	}
}

func publicCaseMediaLabel(media string) string {
	switch media {
	case "application/json", "text/plain", "":
		return media
	default:
		return "unexpected-media-withheld"
	}
}

func appendFields(base []FormField, extra ...FormField) []FormField {
	return append(append([]FormField(nil), base...), extra...)
}

// The pinned engine permits positive-temperature fallback even when the public
// request starts at zero; a single lexical token is not a deterministic API
// promise. Recognizability still requires independent whole-word identity and
// call-to-action cues from this known public fixture, not arbitrary nonempty ASR.
func identifiedPublicSpeech(text string) bool {
	facts := observePublicSpeech(text)
	identity := facts.Fellow && (facts.Americans || facts.American)
	clause := facts.What && (facts.Your || facts.You)
	action := facts.Ask || (facts.Can && facts.Do) || facts.Country || facts.Countries
	return identity && clause && action
}

func checkPublicOption(reply PublicReply, requestErr error, expectedMedia, observedMedia string, requests int32, recognizeSpeech bool) error {
	if requestErr != nil || reply.Status != http.StatusOK || strings.TrimSpace(reply.Text) == "" || expectedMedia != observedMedia || requests != 1 {
		return errors.New("supported option failed transport, transcript, representation or single-call assertion")
	}
	if recognizeSpeech {
		return checkPublicCase(reply, requestErr, http.StatusOK, "")
	}
	return nil
}

func checkPublicCase(reply PublicReply, requestErr error, status int, code string) error {
	if requestErr != nil || reply.Status != status {
		return errors.New("public case transport/status mismatch")
	}
	if status == http.StatusOK {
		if strings.TrimSpace(reply.Text) == "" || !identifiedPublicSpeech(reply.Text) {
			return errors.New("identified public speech was not transcribed")
		}
	} else if reply.ErrorCode != code {
		return errors.New("public case error classification mismatch")
	}
	return nil
}

// The checksum is consumed alongside actual audio, not as a repository-layout test.
func readPublicCaseFixture(dir, name string) ([]byte, error) {
	manifest, err := os.Open(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return nil, errors.New("public fixture identities unavailable")
	}
	content, err := io.ReadAll(io.LimitReader(manifest, 64<<10+1))
	_ = manifest.Close()
	if err != nil || len(content) > 64<<10 {
		return nil, errors.New("public fixture identity input exceeded its bound")
	}
	expected := ""
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && filepath.Base(fields[1]) == name {
			if expected != "" {
				return nil, errors.New("ambiguous public fixture identity")
			}
			expected = fields[0]
		}
	}
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size || name == "jfk.wav" && expected != publicJFKIdentity {
		return nil, errors.New("public fixture identity is invalid")
	}
	file, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return nil, errors.New("public fixture bytes unavailable")
	}
	defer file.Close()
	audio, err := io.ReadAll(io.LimitReader(file, publicFixtureLimit+1))
	digest := sha256.Sum256(audio)
	if err != nil || len(audio) == 0 || int64(len(audio)) > publicFixtureLimit || hex.EncodeToString(digest[:]) != expected {
		return nil, errors.New("public fixture size or checksum mismatch")
	}
	return audio, nil
}

type publicCaseWire struct {
	next     http.RoundTripper
	requests atomic.Int32
	media    atomic.Value
}

func (w *publicCaseWire) RoundTrip(request *http.Request) (*http.Response, error) {
	w.requests.Add(1)
	response, err := w.next.RoundTrip(request)
	if response != nil {
		w.media.Store(response.Header.Get("Content-Type"))
	}
	return response, err
}
func (w *publicCaseWire) mediaType() string {
	value, ok := w.media.Load().(string)
	if !ok {
		return ""
	}
	media, _, _ := mime.ParseMediaType(value)
	return media
}

func sendPublicCaseRaw(ctx context.Context, client *PublicClient, body []byte, contentType string) (PublicReply, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(body))
	if err != nil {
		return PublicReply{}, errors.New("cannot assemble negative public request")
	}
	request.Header.Set("Content-Type", contentType)
	response, err := client.client.Do(request)
	if err != nil {
		return PublicReply{}, &publicRequestError{err}
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, client.maxReplyBytes+1))
	if err != nil || int64(len(content)) > client.maxReplyBytes {
		return PublicReply{}, errors.New("negative public response exceeded observation bound")
	}
	var result struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(content, &result) != nil {
		return PublicReply{}, errors.New("negative public response is not a JSON error")
	}
	return PublicReply{Status: response.StatusCode, ErrorCode: result.Error.Code}, nil
}

func TestPublicSpeechObservationRetainsOnlySafeFacts(t *testing.T) {
	text := "PRIVATE_RESPONSE_MARKER Ask NOT what your fellow AMERICANS can do for countries."
	facts := observePublicSpeech(text)
	encoded, err := json.Marshal(facts)
	if err != nil || strings.Contains(string(encoded), "PRIVATE_RESPONSE_MARKER") || strings.Contains(string(encoded), text) {
		t.Fatal("raw speech escaped the safe observation")
	}
	digest := sha256.Sum256([]byte(text))
	if facts.Bytes != len(text) || facts.Runes != utf8.RuneCountInString(text) || facts.SHA256 != hex.EncodeToString(digest[:]) || !facts.Ask || !facts.Not || !facts.What || !facts.Your || !facts.Fellow || !facts.Americans || !facts.Can || !facts.Do || !facts.Countries || facts.Country {
		t.Fatal("allowlisted word facts, content identity or lengths are inaccurate")
	}
}

func TestPublicCaseRecognizedPhraseViaHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		valid      bool
	}{
		{"complete-phrase", "My fellow Americans, ask not what your country can do for you.", true},
		{"recognized-without-country", "My fellow American, ask what you can do.", true},
		{"recognized-without-ask-or-country", "My fellow Americans, what you can do.", true},
		{"recognized-country-clause", "My fellow American, what your country needs.", true},
		{"country-alone", "country", false},
		{"missing-identity", "Ask what you can do for your country.", false},
		{"missing-call-to-action", "My fellow Americans, your country.", false},
		{"word-substrings", "fellowship Americans askance whatever yourself country", false},
		{"identity-substring", "Fellowship Americans, ask what you can do.", false},
		{"call-substring", "My fellow Americans, askance what you say.", false},
		{"missing-action-cue", "My fellow Americans, what you say.", false},
		{"what-substring", "My fellow Americans, whatever you can do.", false},
		{"pronoun-substring", "My fellow Americans, what yourself can do.", false},
		{"unrelated", "Civic responsibility.", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(struct {
					Text string `json:"text"`
				}{tc.text})
			}))
			defer server.Close()
			client, err := NewPublicClient(server.URL, time.Second, 4096)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			reply, err := client.Transcribe(t.Context(), []byte("synthetic transport-only audio"), "public.wav", []FormField{{"model", "base"}})
			if (checkPublicCase(reply, err, http.StatusOK, "") == nil) != tc.valid {
				t.Fatal("recognized phrase oracle accepted wrong speech or rejected a valid country-free variant")
			}
		})
	}
}

func TestPublicCaseTemperatureOneChecksProtocolNotLexicalQuality(t *testing.T) {
	for _, tc := range []struct {
		name, body, media string
		status            int
		requests          int32
		valid             bool
	}{
		{"numeric-bound-accepted", `{"text":"Variable output."}`, "application/json", http.StatusOK, 1, true},
		{"empty", `{"text":""}`, "application/json", http.StatusOK, 1, false},
		{"failed-http", `{"error":{"message":"safe","type":"service_unavailable_error","param":null,"code":"not_ready"}}`, "application/json", http.StatusServiceUnavailable, 1, false},
		{"wrong-media", "Variable output.", "text/plain", http.StatusOK, 1, false},
		{"two-requests", `{"text":"Variable output."}`, "application/json", http.StatusOK, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.media)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client, err := NewPublicClient(server.URL, time.Second, 4096)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			wire := &publicCaseWire{next: client.client.Transport}
			client.client.Transport = wire
			var reply PublicReply
			for i := int32(0); i < tc.requests; i++ {
				reply, err = client.Transcribe(t.Context(), []byte("synthetic transport-only audio"), "public.wav", []FormField{{"model", "base"}, {"temperature", "1"}})
			}
			if (checkPublicOption(reply, err, "application/json", wire.mediaType(), wire.requests.Load(), false) == nil) != tc.valid {
				t.Fatal("temperature-one contract acceptance or required protocol guard is incorrect")
			}
			if tc.valid && checkPublicOption(reply, err, "application/json", wire.mediaType(), wire.requests.Load(), true) == nil {
				t.Fatal("temperature-zero/default recognized speech requirement was relaxed")
			}
		})
	}
}

func TestPublicCaseRecognizedOptionRetainsProtocolChecks(t *testing.T) {
	// Validate the outcome guard independently of PublicClient's current choice
	// not to retain text on errors: a transcript can never override HTTP failure.
	if checkPublicOption(PublicReply{Status: http.StatusServiceUnavailable, Text: "Civic responsibility."}, nil, "application/json", "application/json", 1, true) == nil {
		t.Fatal("stochastic option lost protocol status requirement")
	}

	for _, tc := range []struct {
		name, body, media string
		status            int
		requests          int32
		valid             bool
	}{
		{"recognized-variable-speech", `{"text":"My fellow American, ask what you can do."}`, "application/json", http.StatusOK, 1, true},
		{"empty-speech", `{"text":""}`, "application/json", http.StatusOK, 1, false},
		{"http-error", `{"error":{"message":"safe","type":"service_unavailable_error","param":null,"code":"not_ready"}}`, "application/json", http.StatusServiceUnavailable, 1, false},
		{"wrong-representation", "My fellow American, ask what you can do.", "text/plain", http.StatusOK, 1, false},
		{"duplicate-calls", `{"text":"My fellow American, ask what you can do."}`, "application/json", http.StatusOK, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.media)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client, err := NewPublicClient(server.URL, time.Second, 4096)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			wire := &publicCaseWire{next: client.client.Transport}
			client.client.Transport = wire
			var reply PublicReply
			for i := int32(0); i < tc.requests; i++ {
				reply, err = client.Transcribe(t.Context(), []byte("synthetic transport-only audio"), "public.wav", []FormField{{"model", "base"}, {"temperature", "1"}})
			}
			if (checkPublicOption(reply, err, "application/json", wire.mediaType(), wire.requests.Load(), true) == nil) != tc.valid {
				t.Fatal("stochastic option lost protocol, nonempty or single-call requirements")
			}
		})
	}
}

func TestPublicCaseErrorStatusIsNotInterchangeable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"message":"safe","type":"invalid_request_error","param":null,"code":"invalid_request"}}`)
	}))
	defer server.Close()
	client, err := NewPublicClient(server.URL, time.Second, 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reply, err := client.Transcribe(t.Context(), []byte("synthetic transport-only audio"), "public.wav", nil)
	if checkPublicCase(reply, err, http.StatusBadRequest, "invalid_request") == nil {
		t.Fatal("wrong HTTP status accepted for rejection")
	}
}

func TestPublicCaseRawNegativeReachesHTTP(t *testing.T) {
	var reached atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Add(1)
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil || string(body) != "truncated actual negative request" {
			t.Error("negative input was altered before HTTP")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"code":"invalid_request"}}`)
	}))
	defer server.Close()
	client, err := NewPublicClient(server.URL, time.Second, 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	reply, err := sendPublicCaseRaw(t.Context(), client, []byte("truncated actual negative request"), "multipart/form-data")
	if reached.Load() != 1 || checkPublicCase(reply, err, http.StatusBadRequest, "invalid_request") != nil {
		t.Fatal("raw negative short-circuited instead of consuming actual HTTP rejection")
	}
}

func TestPublicCaseAssertionsUseHTTPOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		shouldPass bool
	}{{"identified", `{"text":"My fellow Americans, ask what you can do for your country."}`, http.StatusOK, true}, {"unrelated", `{"text":"unrelated speech"}`, http.StatusOK, false}, {"busy", `{"error":{"message":"PRIVATE_MARKER","type":"service_unavailable_error","param":null,"code":"not_ready"}}`, http.StatusServiceUnavailable, false}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			client, err := NewPublicClient(server.URL, time.Second, 4096)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			reply, err := client.Transcribe(t.Context(), []byte("synthetic transport-only fixture"), "public.wav", []FormField{{"model", "base"}})
			if (checkPublicCase(reply, err, http.StatusOK, "") == nil) != tc.shouldPass {
				t.Fatal("actual HTTP outcome did not discriminate valid identified speech from failed/unrelated outcomes")
			}
		})
	}
	if checkPublicCase(PublicReply{Status: http.StatusBadRequest, ErrorCode: "invalid_request"}, nil, http.StatusBadRequest, "invalid_request") != nil || checkPublicCase(PublicReply{Status: http.StatusBadRequest, ErrorCode: "unsupported_audio"}, nil, http.StatusBadRequest, "invalid_request") == nil || checkPublicCase(PublicReply{}, errors.New("synthetic assembly failure"), http.StatusBadRequest, "invalid_request") == nil {
		t.Fatal("negative assertion accepted wrong classification or client-side failure")
	}
}
