package domain

import "testing"

func TestCanonicalLanguage(t *testing.T) {
	for _, tc := range []struct {
		input string
		code  string
	}{
		{"auto", "auto"},
		{"en", "en"},
		{"english", "en"},
		{"ru", "ru"},
		{"russian", "ru"},
		{"haitian creole", "ht"},
		{"yue", "yue"},
		{"cantonese", "yue"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			code, ok := CanonicalLanguage(tc.input)
			if !ok || code != tc.code {
				t.Fatalf("language %q: got %q, %v", tc.input, code, ok)
			}
			if !WhisperLanguage(code) {
				t.Fatalf("canonical language %q is not accepted", code)
			}
		})
	}

	for _, input := range []string{"", "EN", "English", "en-US", " english", "unknown", "../en"} {
		if code, ok := CanonicalLanguage(input); ok || code != "" || WhisperLanguage(input) {
			t.Errorf("invalid language %q accepted as %q", input, code)
		}
	}
	if WhisperLanguage("english") {
		t.Fatal("language name accepted as a native code before canonicalization")
	}
}
