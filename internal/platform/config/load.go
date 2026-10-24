package config

import (
	_ "embed"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

//go:embed defaults.json
var defaults []byte

// Load returns owned typed settings without starting a listener or process.
// A nil environ uses os.Environ; an empty slice disables environment overrides.
func Load(args, environ []string) (Settings, error) {
	bindings := settingsBindings()
	path, flags, err := commandLine(args, bindings)
	if err != nil {
		return Settings{}, err
	}

	k := koanf.New(".")
	if err := loadJSON(k, defaults, bindings); err != nil {
		return Settings{}, err
	}
	if path != "" {
		data, err := file.Provider(path).ReadBytes()
		if err != nil || loadJSON(k, data, bindings) != nil {
			return Settings{}, ErrInvalid
		}
	}

	variables, err := environment(environ, bindings)
	if err != nil {
		return Settings{}, err
	}
	for _, provider := range []koanf.Provider{confmap.Provider(variables, ""), flags} {
		if err := mergeSource(k, provider, bindings); err != nil {
			return Settings{}, err
		}
	}

	result, err := decode(k)
	if err != nil || result.Validate() != nil {
		return Settings{}, ErrInvalid
	}
	return result, nil
}

func loadJSON(k *koanf.Koanf, data []byte, bindings []binding) error {
	var values map[string]any
	if err := readJSON(data, &values); err != nil {
		return err
	}
	return mergeSource(k, confmap.Provider(values, ""), bindings)
}

// Each source is decoded and validated before its values can be overwritten.
func mergeSource(k *koanf.Koanf, provider koanf.Provider, bindings []binding) error {
	source := koanf.New(".")
	if err := source.Load(provider, nil); err != nil {
		return ErrInvalid
	}

	values, err := decode(source)
	if err != nil || validatePartial(source, values, bindings) != nil {
		return ErrInvalid
	}

	// Koanf replaces slices, including explicitly empty collections.
	if err := k.Merge(source); err != nil {
		return ErrInvalid
	}
	return nil
}

func decode(k *koanf.Koanf) (Settings, error) {
	var result Settings
	err := k.UnmarshalWithConf("", &result, koanf.UnmarshalConf{
		DecoderConfig: &mapstructure.DecoderConfig{
			ErrorUnused: true,
			MatchName: func(key, field string) bool {
				return key == field
			},
			DecodeHook: strictNumber,
		},
	})
	if err != nil {
		return Settings{}, ErrInvalid
	}
	return result, nil
}
