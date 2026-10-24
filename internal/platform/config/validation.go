package config

import (
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/knadh/koanf/v2"
)

// mapstructure decodes json.Number natively; do not let its string kind
// turn JSON numbers into strings such as hostnames or model IDs.
func strictNumber(from, to reflect.Type, value any) (any, error) {
	if from == reflect.TypeFor[json.Number]() && to.Kind() != reflect.Int && to.Kind() != reflect.Int64 && to.Kind() != reflect.Float64 {
		return nil, ErrInvalid
	}
	return value, nil
}

func validatePartial(part *koanf.Koanf, values Settings, bindings []binding) error {
	if err := validatePartialFields(part, values, bindings); err != nil {
		return err
	}
	if err := validatePartialModels(part, values.Models); err != nil {
		return err
	}
	return validatePartialConstraints(part, values)
}

func validatePartialFields(part *koanf.Koanf, values Settings, bindings []binding) error {
	value := reflect.ValueOf(values)
	for _, b := range bindings {
		if !part.Exists(b.path) {
			continue
		}
		field := value.FieldByIndex(b.index)
		if fields.Var(field.Interface(), b.rule) != nil {
			return ErrInvalid
		}
		if strings.HasSuffix(b.path, "Seconds") && field.Int() > maxSeconds {
			return ErrInvalid
		}
		if field.Kind() == reflect.Float64 && (math.IsNaN(field.Float()) || math.IsInf(field.Float(), 0)) {
			return ErrInvalid
		}
	}
	return nil
}

func validatePartialModels(part *koanf.Koanf, models Models) error {
	if part.Exists("models.catalog") {
		for _, m := range models.Catalog {
			if validateModel(m) != nil {
				return ErrInvalid
			}
		}
		if err := models.validateReferences(part.Exists("models.served"), part.Exists("models.downloadSet")); err != nil {
			return err
		}
	}
	if part.Exists("models.served") && !identifier.MatchString(models.Served) {
		return ErrInvalid
	}
	if part.Exists("models.downloadSet") {
		for _, id := range models.DownloadSet {
			if !identifier.MatchString(id) {
				return ErrInvalid
			}
		}
	}
	return nil
}

func validatePartialConstraints(part *koanf.Koanf, values Settings) error {
	if part.Exists("api.host") && !validHost(values.API.Host) {
		return ErrInvalid
	}
	if part.Exists("api.backendUrl") && !validBackendURL(values.API.BackendURL) {
		return ErrInvalid
	}
	if part.Exists("storage.cacheDir") && !filepath.IsAbs(values.Storage.CacheDir) {
		return ErrInvalid
	}
	if part.Exists("backend.port") && part.Exists("backend.nativePort") && values.Backend.Port == values.Backend.NativePort {
		return ErrInvalid
	}
	if part.Exists("api.rateLimit.idleSeconds") && part.Exists("api.rateLimit.burst") && part.Exists("api.rateLimit.requestsPerSecond") {
		if float64(values.API.RateLimit.IdleSeconds) < float64(values.API.RateLimit.Burst)/values.API.RateLimit.RequestsPerSecond {
			return ErrInvalid
		}
	}
	if part.Exists("api.clientIP.trustedProxyCIDRs") {
		proxy := ClientIP{Source: "direct", TrustedProxyCIDRs: values.API.ClientIP.TrustedProxyCIDRs}
		if len(proxy.TrustedProxyCIDRs) > 0 {
			proxy.Source = "x-forwarded-for"
		}
		if proxy.Validate() != nil {
			return ErrInvalid
		}
		if part.Exists("api.clientIP.source") {
			proxy.Source = values.API.ClientIP.Source
			if proxy.Validate() != nil {
				return ErrInvalid
			}
		}
	}
	return nil
}
