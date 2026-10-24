package httpapi

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/config"
	"github.com/swimmwatch/openwhispr-whisper-server/internal/platform/logging"
)

func NewMemoryStore(settings config.RateLimit) *middleware.RateLimiterMemoryStore {
	return middleware.NewRateLimiterMemoryStoreWithConfig(middleware.RateLimiterMemoryStoreConfig{
		Rate:      settings.RequestsPerSecond,
		Burst:     settings.Burst,
		ExpiresIn: time.Duration(settings.IdleSeconds) * time.Second,
	})
}

// Install configures the reusable boundary; commands own routes and lifecycle.
func Install(e *echo.Echo, logger *slog.Logger, settings config.API, store middleware.RateLimiterStore) error {
	if logger == nil ||
		settings.RateLimit.Validate() != nil ||
		settings.ClientIP.Validate() != nil ||
		(settings.RateLimit.Enabled && store == nil) {
		return errors.New("invalid HTTP middleware configuration")
	}

	extractor, err := clientIPExtractor(settings.ClientIP)
	if err != nil {
		return err
	}

	e.IPExtractor = extractor
	e.HTTPErrorHandler = ErrorHandler
	e.Use(requestID(), requestLogger(logger), safeRecover())
	if settings.RateLimit.Enabled {
		e.Use(rateLimiter(store))
	}
	return nil
}

func clientIPExtractor(settings config.ClientIP) (func(*http.Request) string, error) {
	extractor := echo.ExtractIPDirect()
	if settings.Source == "x-forwarded-for" {
		trust := []echo.TrustOption{
			echo.TrustLoopback(false),
			echo.TrustLinkLocal(false),
			echo.TrustPrivateNet(false),
		}
		for _, cidr := range settings.TrustedProxyCIDRs {
			_, network, err := net.ParseCIDR(cidr)
			if err != nil {
				return nil, config.ErrInvalid
			}
			trust = append(trust, echo.TrustIPRange(network))
		}
		extractor = echo.ExtractIPFromXFFHeader(trust...)
	}

	return func(r *http.Request) string {
		ip, err := netip.ParseAddr(extractor(r))
		if err != nil || ip.Zone() != "" {
			return ""
		}
		return ip.Unmap().String()
	}, nil
}

func requestLogger(logger *slog.Logger) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		HandleError:  true,
		LogLatency:   true,
		LogMethod:    true,
		LogRoutePath: true,
		LogStatus:    true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			code := ""
			if v.Error != nil {
				code = MapFailure(v.Error).Code
			}

			level := slog.LevelInfo
			if v.Status >= http.StatusInternalServerError {
				level = slog.LevelError
			} else if v.Status >= http.StatusBadRequest {
				level = slog.LevelWarn
			}

			status := v.Status
			if !responseCommitted(c) && c.Request().Context().Err() != nil {
				status = 0
			}

			logging.Emit(logger, c.Request().Context(), level, logging.Event{
				Name:       logging.RequestCompleted,
				Method:     v.Method,
				Route:      v.RoutePath,
				Status:     status,
				DurationMS: v.Latency.Milliseconds(),
				ErrorCode:  code,
			})
			return nil
		},
	})
}

func rateLimiter(store middleware.RateLimiterStore) echo.MiddlewareFunc {
	return middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: closedStore{store},
		Skipper: func(c *echo.Context) bool {
			return c.Request().Method != http.MethodPost || c.Path() != transcriptionsPath
		},
		IdentifierExtractor: func(c *echo.Context) (string, error) {
			id := c.RealIP()
			if id == "" {
				return "", config.ErrInvalid
			}
			return id, nil
		},
		ErrorHandler: func(c *echo.Context, err error) error {
			return NewHTTPFailure("invalid_request", "")
		},
		DenyHandler: func(c *echo.Context, id string, err error) error {
			if err != nil {
				c.Response().Header().Del(echo.HeaderRetryAfter)
				c.Response().Header().Del(middleware.HeaderXRateLimitLimit)
				c.Response().Header().Del(middleware.HeaderXRateLimitRemaining)
				return NewHTTPFailure("rate_limit_unavailable", "")
			}

			failure := NewHTTPFailure("rate_limit_exceeded", "")
			retry, parseErr := strconv.Atoi(c.Response().Header().Get(echo.HeaderRetryAfter))
			if parseErr != nil || retry < minRetryAfterSeconds {
				retry = minRetryAfterSeconds
			}
			failure.RetryAfter = retry
			return failure
		},
	})
}

func requestID() echo.MiddlewareFunc {
	correlation := middleware.RequestIDWithConfig(middleware.RequestIDConfig{RequestIDHandler: func(c *echo.Context, id string) {
		c.Request().Header.Set(echo.HeaderXRequestID, id)
		c.SetRequest(c.Request().WithContext(logging.WithRequestID(c.Request().Context(), id)))
	}})

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		withID := correlation(next)
		return func(c *echo.Context) error {
			values := c.Request().Header.Values(echo.HeaderXRequestID)
			if len(values) != 1 || !logging.ValidRequestID(values[0]) {
				c.Request().Header.Del(echo.HeaderXRequestID)
			}
			return withID(c)
		}
	}
}

var recoveredPanic = errors.New("internal panic")

func safeRecover() echo.MiddlewareFunc {
	recovery := middleware.RecoverWithConfig(middleware.RecoverConfig{DisablePrintStack: true, DisableStackAll: true})
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		// Replace the value before Echo can format it or preserve a typed service error.
		return recovery(func(c *echo.Context) (err error) {
			defer func() {
				if value := recover(); value != nil {
					if value == http.ErrAbortHandler {
						panic(value)
					}
					panic(recoveredPanic)
				}
			}()
			return next(c)
		})
	}
}

// closedStore preserves Echo's interface and normalizes errors to denied admission.
type closedStore struct{ store middleware.RateLimiterStore }

func (s closedStore) Allow(id string) (bool, error) {
	allowed, err := s.store.Allow(id)
	return allowed && err == nil, err
}

func (s closedStore) AllowContext(c *echo.Context, id string) (bool, error) {
	if contextual, ok := s.store.(middleware.RateLimiterStoreContext); ok {
		allowed, err := contextual.AllowContext(c, id)
		return allowed && err == nil, err
	}
	return s.Allow(id)
}
