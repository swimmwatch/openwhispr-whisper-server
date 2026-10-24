package config

import (
	"errors"
	"math"
	"net/netip"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

type Settings struct {
	SchemaVersion int       `json:"schema_version" koanf:"schema_version" validate:"eq=1" env:"SCHEMA_VERSION" flag:"schema-version"`
	Mode          string    `json:"mode" koanf:"mode" validate:"eq=resident" env:"MODE" flag:"mode"`
	Models        Models    `json:"models" koanf:"models"`
	API           API       `json:"api" koanf:"api"`
	Backend       Backend   `json:"backend" koanf:"backend"`
	Storage       Storage   `json:"storage" koanf:"storage"`
	Queue         Queue     `json:"queue" koanf:"queue"`
	Limits        Limits    `json:"limits" koanf:"limits"`
	Timeouts      Timeouts  `json:"timeouts" koanf:"timeouts"`
	Downloads     Downloads `json:"downloads" koanf:"downloads"`
	Logging       Logging   `json:"logging" koanf:"logging"`
}

type Models struct {
	Catalog     []Model  `json:"catalog" koanf:"catalog" validate:"required,min=1,unique=ID,dive" env:"CATALOG_JSON" flag:"catalog-json"`
	Served      string   `json:"served" koanf:"served" validate:"required" env:"SERVED_MODEL" flag:"served-model"`
	DownloadSet []string `json:"downloadSet" koanf:"downloadSet" validate:"unique" env:"DOWNLOAD_SET_JSON" flag:"download-set-json"`
}

type Model struct {
	ID     string `json:"id" koanf:"id" validate:"required"`
	Source Source `json:"source" koanf:"source"`
}

type Source struct {
	Kind          string `json:"kind" koanf:"kind" validate:"oneof=download prepared"`
	URL           string `json:"url,omitempty" koanf:"url"`
	Path          string `json:"path,omitempty" koanf:"path"`
	SHA256        string `json:"sha256,omitempty" koanf:"sha256" validate:"omitempty,sha256"`
	ExpectedBytes int64  `json:"expected_bytes,omitempty" koanf:"expected_bytes" validate:"gte=0"`
}

type API struct {
	Host       string    `json:"host" koanf:"host" validate:"required" env:"API_HOST" flag:"api-host"`
	Port       int       `json:"port" koanf:"port" validate:"gte=1,lte=65535" env:"API_PORT" flag:"api-port"`
	BackendURL string    `json:"backendUrl" koanf:"backendUrl" validate:"required" env:"BACKEND_URL" flag:"backend-url"`
	RateLimit  RateLimit `json:"rateLimit" koanf:"rateLimit"`
	ClientIP   ClientIP  `json:"clientIP" koanf:"clientIP"`
}

type RateLimit struct {
	Enabled           bool    `json:"enabled" koanf:"enabled" env:"RATE_LIMIT_ENABLED" flag:"rate-limit-enabled"`
	Store             string  `json:"store" koanf:"store" validate:"eq=memory" env:"RATE_LIMIT_STORE" flag:"rate-limit-store"`
	RequestsPerSecond float64 `json:"requestsPerSecond" koanf:"requestsPerSecond" validate:"gt=0" env:"RATE_LIMIT_REQUESTS_PER_SECOND" flag:"rate-limit-requests-per-second"`
	Burst             int     `json:"burst" koanf:"burst" validate:"gt=0" env:"RATE_LIMIT_BURST" flag:"rate-limit-burst"`
	IdleSeconds       int     `json:"idleSeconds" koanf:"idleSeconds" validate:"gt=0" env:"RATE_LIMIT_IDLE_SECONDS" flag:"rate-limit-idle-seconds"`
}

type ClientIP struct {
	Source            string   `json:"source" koanf:"source" validate:"oneof=direct x-forwarded-for" env:"CLIENT_IP_SOURCE" flag:"client-ip-source"`
	TrustedProxyCIDRs []string `json:"trustedProxyCIDRs" koanf:"trustedProxyCIDRs" env:"TRUSTED_PROXY_CIDRS_JSON" flag:"trusted-proxy-cidrs-json"`
}

type Backend struct {
	Port       int `json:"port" koanf:"port" validate:"gte=1,lte=65535" env:"BACKEND_PORT" flag:"backend-port"`
	NativePort int `json:"nativePort" koanf:"nativePort" validate:"gte=1,lte=65535" env:"NATIVE_PORT" flag:"native-port"`
	GPUDevice  int `json:"gpuDevice" koanf:"gpuDevice" validate:"gte=0" env:"GPU_DEVICE" flag:"gpu-device"`
}

type Storage struct {
	CacheDir string `json:"cacheDir" koanf:"cacheDir" validate:"required" env:"CACHE_DIR" flag:"cache-dir"`
}

type Queue struct {
	WaitingCapacity int `json:"waitingCapacity" koanf:"waitingCapacity" validate:"gte=0" env:"WAITING_CAPACITY" flag:"waiting-capacity"`
}

type Limits struct {
	AudioBytes     int64 `json:"audioBytes" koanf:"audioBytes" validate:"gt=0" env:"AUDIO_BYTES" flag:"audio-bytes"`
	PromptBytes    int   `json:"promptBytes" koanf:"promptBytes" validate:"gt=0" env:"PROMPT_BYTES" flag:"prompt-bytes"`
	DecodedSeconds int   `json:"decodedSeconds" koanf:"decodedSeconds" validate:"gt=0" env:"DECODED_SECONDS" flag:"decoded-seconds"`
}

type Timeouts struct {
	OperationSeconds    int `json:"operationSeconds" koanf:"operationSeconds" validate:"gt=0" env:"OPERATION_SECONDS" flag:"operation-seconds"`
	CleanupSeconds      int `json:"cleanupSeconds" koanf:"cleanupSeconds" validate:"gt=0" env:"CLEANUP_SECONDS" flag:"cleanup-seconds"`
	ShutdownSeconds     int `json:"shutdownSeconds" koanf:"shutdownSeconds" validate:"gt=0" env:"SHUTDOWN_SECONDS" flag:"shutdown-seconds"`
	ProvisioningSeconds int `json:"provisioningSeconds" koanf:"provisioningSeconds" validate:"gt=0" env:"PROVISIONING_SECONDS" flag:"provisioning-seconds"`
}

type Downloads struct {
	Attempts int `json:"attempts" koanf:"attempts" validate:"gt=0" env:"DOWNLOAD_ATTEMPTS" flag:"download-attempts"`
}

type Logging struct {
	Level string `json:"level" koanf:"level" validate:"oneof=debug info warn error" env:"LOG_LEVEL" flag:"log-level"`
}

var fields = validator.New(validator.WithRequiredStructEnabled())
var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

const (
	maxSeconds    = int64(math.MaxInt64) / int64(time.Second)
	maxHostLength = 253
	maxPort       = 65535
)

// ErrInvalid is deliberately bounded; raw values and paths are never diagnostic output.
var ErrInvalid = errors.New("invalid configuration")

func (c Settings) Validate() error {
	if err := fields.Struct(c); err != nil {
		return ErrInvalid
	}
	if err := c.API.RateLimit.Validate(); err != nil {
		return err
	}
	if err := c.API.ClientIP.Validate(); err != nil {
		return err
	}
	if c.Backend.Port == c.Backend.NativePort || !filepath.IsAbs(c.Storage.CacheDir) {
		return ErrInvalid
	}
	if !validHost(c.API.Host) {
		return ErrInvalid
	}
	if !validBackendURL(c.API.BackendURL) {
		return ErrInvalid
	}
	return c.Models.Validate()
}

func (m Models) Validate() error {
	if fields.Struct(m) != nil || !identifier.MatchString(m.Served) {
		return ErrInvalid
	}

	for _, model := range m.Catalog {
		if err := validateModel(model); err != nil {
			return ErrInvalid
		}
	}
	return m.validateReferences(true, true)
}

func (m Models) validateReferences(served, downloads bool) error {
	sources := make(map[string]string, len(m.Catalog))
	for _, model := range m.Catalog {
		sources[model.ID] = model.Source.Kind
	}
	if served && sources[m.Served] == "" {
		return ErrInvalid
	}
	if downloads {
		for _, id := range m.DownloadSet {
			if sources[id] != "download" {
				return ErrInvalid
			}
		}
	}
	return nil
}

func (r RateLimit) Validate() error {
	if fields.Struct(r) != nil ||
		int64(r.IdleSeconds) > maxSeconds ||
		math.IsNaN(r.RequestsPerSecond) ||
		math.IsInf(r.RequestsPerSecond, 0) ||
		float64(r.IdleSeconds) < float64(r.Burst)/r.RequestsPerSecond {
		return ErrInvalid
	}
	return nil
}

func validHost(value string) bool {
	if ip, err := netip.ParseAddr(value); err == nil {
		return ip.Zone() == ""
	}
	return len(value) <= maxHostLength && fields.Var(strings.TrimSuffix(value, "."), "hostname_rfc1123") == nil
}

func validBackendURL(value string) bool {
	u, err := url.Parse(value)
	if err != nil ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		!validHost(u.Hostname()) ||
		u.User != nil ||
		u.RawQuery != "" ||
		u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > maxPort {
			return false
		}
	}
	return true
}

func (c ClientIP) Validate() error {
	if fields.Struct(c) != nil ||
		(c.Source == "direct" && len(c.TrustedProxyCIDRs) != 0) ||
		(c.Source == "x-forwarded-for" && len(c.TrustedProxyCIDRs) == 0) {
		return ErrInvalid
	}

	seen := map[netip.Prefix]bool{}
	for _, cidr := range c.TrustedProxyCIDRs {
		p, err := netip.ParsePrefix(cidr)
		if err != nil || p.Bits() == 0 || p.Addr().Is4In6() || seen[p.Masked()] {
			return ErrInvalid
		}
		seen[p.Masked()] = true
	}
	return nil
}

func validateModel(m Model) error {
	if !identifier.MatchString(m.ID) || fields.Struct(m) != nil {
		return ErrInvalid
	}

	s := m.Source
	switch s.Kind {
	case "download":
		u, err := url.Parse(s.URL)
		if err != nil ||
			u.Scheme != "https" ||
			u.Hostname() == "" ||
			u.User != nil ||
			u.Fragment != "" ||
			s.Path != "" ||
			s.SHA256 == "" {
			return ErrInvalid
		}
	case "prepared":
		if !filepath.IsAbs(s.Path) || s.URL != "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
