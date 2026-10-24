package config

import (
	"bytes"
	"encoding/json"
	"io"
)

const maxJSONDepth = 100

// Precheck before decoding: encoding/json otherwise discards duplicate keys.
func readJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := checkTokens(decoder); err != nil {
		return err
	}

	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return ErrInvalid
	}
	return nil
}

// checkTokens rejects duplicate properties before decoding can discard them.
func checkTokens(dec *json.Decoder) error {
	if err := checkJSONValue(dec, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func checkJSONValue(dec *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return ErrInvalid
	}

	token, err := dec.Token()
	if err != nil || token == nil {
		return ErrInvalid
	}

	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delimiter {
	case '{':
		if err := checkJSONObject(dec, depth); err != nil {
			return err
		}
	case '[':
		for dec.More() {
			if err := checkJSONValue(dec, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}

	end, err := dec.Token()
	if err != nil || (delimiter == '{' && end != json.Delim('}')) || (delimiter == '[' && end != json.Delim(']')) {
		return ErrInvalid
	}
	return nil
}

func checkJSONObject(dec *json.Decoder, depth int) error {
	seen := map[string]bool{}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return ErrInvalid
		}
		name, ok := key.(string)
		if !ok || seen[name] {
			return ErrInvalid
		}
		seen[name] = true
		if err := checkJSONValue(dec, depth+1); err != nil {
			return err
		}
	}
	return nil
}
