package config

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/knadh/koanf/providers/basicflag"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/v2"
)

const environmentPrefix = "WHISPER_"

type binding struct {
	path, env, flag, rule string
	typ                   reflect.Type
	index                 []int
}

// Read the existing tags once per load for overrides and per-source validation.
func settingsBindings() []binding {
	var result []binding
	var walk func(reflect.Type, string, []int)

	walk = func(t reflect.Type, prefix string, parent []int) {
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			path := prefix + field.Tag.Get("koanf")
			index := append(append([]int(nil), parent...), i)
			if field.Type.Kind() == reflect.Struct {
				walk(field.Type, path+".", index)
			} else if field.Tag.Get("flag") != "" {
				result = append(result, binding{
					path: path, env: environmentPrefix + field.Tag.Get("env"),
					flag: field.Tag.Get("flag"), rule: field.Tag.Get("validate"),
					typ: field.Type, index: index,
				})
			}
		}
	}

	walk(reflect.TypeFor[Settings](), "", nil)
	return result
}

func commandLine(args []string, bindings []binding) (string, koanf.Provider, error) {
	fs := flag.NewFlagSet("whisper", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	path, configSet := "", false
	fs.Func("config", "JSON configuration file", func(value string) error {
		if configSet {
			return ErrInvalid
		}
		path, configSet = value, true
		return nil
	})

	provided := map[string]any{}
	paths := map[string]string{}
	for _, b := range bindings {
		paths[b.flag] = b.path
		fs.Func(b.flag, "Explicit setting override", func(value string) error {
			if _, exists := provided[b.flag]; exists {
				return ErrInvalid
			}
			parsed, err := representation(value, b.typ)
			if err == nil {
				provided[b.flag] = parsed
			}
			return err
		})
	}
	if fs.Parse(args) != nil || fs.NArg() != 0 {
		return "", nil, ErrInvalid
	}

	provider := basicflag.ProviderWithValue(fs, ".", func(name, _ string) (string, any) {
		value, explicit := provided[name]
		if !explicit {
			return "", nil
		}
		return paths[name], value
	})
	return path, provider, nil
}

func environment(environ []string, bindings []binding) (map[string]any, error) {
	if environ == nil {
		environ = os.Environ()
	}

	byEnv := map[string]binding{}
	for _, b := range bindings {
		byEnv[b.env] = b
	}

	seen := map[string]bool{}
	for _, entry := range environ {
		name, _, hasValue := strings.Cut(entry, "=")
		if strings.HasPrefix(name, environmentPrefix) {
			if _, known := byEnv[name]; !hasValue || !known || seen[name] {
				return nil, ErrInvalid
			}
			seen[name] = true
		}
	}

	var parseErr error
	provider := env.Provider(".", env.Opt{
		Prefix: environmentPrefix,
		EnvironFunc: func() []string {
			return environ
		},
		TransformFunc: func(name, value string) (string, any) {
			b := byEnv[name]
			parsed, err := representation(value, b.typ)
			if err != nil {
				parseErr = err
			}
			return b.path, parsed
		},
	})

	values, err := provider.Read()
	if err != nil || parseErr != nil {
		return nil, ErrInvalid
	}
	return values, nil
}

// Text overrides use the documented scalar/JSON-array representations.
func representation(value string, t reflect.Type) (any, error) {
	switch t.Kind() {
	case reflect.String:
		return value, nil
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(value, 10, t.Bits())
		if err == nil {
			return json.Number(strconv.FormatInt(n, 10)), nil
		}
	case reflect.Float64:
		n, err := strconv.ParseFloat(value, 64)
		if err == nil {
			return json.Number(strconv.FormatFloat(n, 'g', -1, 64)), nil
		}
	case reflect.Bool:
		if value == "true" || value == "false" {
			return value == "true", nil
		}
	case reflect.Slice:
		var result any
		if err := readJSON([]byte(value), &result); err == nil {
			return result, nil
		}
	}
	return nil, ErrInvalid
}
