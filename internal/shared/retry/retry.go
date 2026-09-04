package retry

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
)

type BackoffStrategy int

const (
	BackoffConstant BackoffStrategy = iota
	BackoffLinear
	BackoffExponential
	BackoffExponentialWithJitter
)

type Config struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	Backoff         BackoffStrategy
	Factor          float64
	RetryableErrors func(error) bool
}

func DefaultConfig() Config {
	return Config{
		MaxAttempts:  3,
		InitialDelay: 500 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Backoff:      BackoffExponentialWithJitter,
		Factor:       2.0,
		RetryableErrors: func(err error) bool {
			var appErr *errors.AppError
			if ok := asAppError(err, &appErr); ok {
				return appErr.Retryable
			}
			return true
		},
	}
}

func asAppError(err error, target **errors.AppError) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(*errors.AppError); ok {
		*target = e
		return true
	}
	return false
}

func isRetryable(err error, cfg Config) bool {
	if cfg.RetryableErrors == nil {
		return true
	}
	return cfg.RetryableErrors(err)
}

func Do(ctx context.Context, cfg Config, operation func() error) error {
	var lastErr error

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			delay := calculateDelay(cfg, attempt)
			select {
			case <-ctx.Done():
				return fmt.Errorf("retry cancelled: %w", ctx.Err())
			case <-time.After(delay):
			}
		}

		err := operation()
		if err == nil {
			return nil
		}

		lastErr = err

		if !isRetryable(err, cfg) {
			return err
		}

		if attempt == cfg.MaxAttempts-1 {
			break
		}
	}

	return fmt.Errorf("all %d attempts failed: %w", cfg.MaxAttempts, lastErr)
}

func calculateDelay(cfg Config, attempt int) time.Duration {
	var delay time.Duration

	switch cfg.Backoff {
	case BackoffConstant:
		delay = cfg.InitialDelay
	case BackoffLinear:
		delay = cfg.InitialDelay + time.Duration(attempt)*cfg.InitialDelay
	case BackoffExponential:
		delay = time.Duration(float64(cfg.InitialDelay) * math.Pow(cfg.Factor, float64(attempt-1)))
	case BackoffExponentialWithJitter:
		base := float64(cfg.InitialDelay) * math.Pow(cfg.Factor, float64(attempt-1))
		jitter := rand.Float64() * base * 0.3
		delay = time.Duration(base + jitter)
	}

	if cfg.MaxDelay > 0 && delay > cfg.MaxDelay {
		delay = cfg.MaxDelay
	}

	return delay
}

type DatabaseRetryConfig struct {
	MaxAttempts int
	Delay       time.Duration
}

func DatabaseOperation(ctx context.Context, cfg DatabaseRetryConfig, operation func() error) error {
	rc := DefaultConfig()
	rc.MaxAttempts = cfg.MaxAttempts
	if cfg.Delay > 0 {
		rc.InitialDelay = cfg.Delay
	}
	rc.RetryableErrors = func(err error) bool {
		var appErr *errors.AppError
		if ok := asAppError(err, &appErr); ok {
			return appErr.Code == errors.ErrDatabase || appErr.Retryable
		}
		return true
	}
	return Do(ctx, rc, operation)
}

type DockerRetryConfig struct {
	MaxAttempts int
	Delay       time.Duration
}

func DockerOperation(ctx context.Context, cfg DockerRetryConfig, operation func() error) error {
	rc := DefaultConfig()
	rc.MaxAttempts = cfg.MaxAttempts
	if cfg.Delay > 0 {
		rc.InitialDelay = cfg.Delay
	}
	rc.RetryableErrors = func(err error) bool {
		var appErr *errors.AppError
		if ok := asAppError(err, &appErr); ok {
			return appErr.Code == errors.ErrDocker || appErr.Retryable
		}
		return false
	}
	return Do(ctx, rc, operation)
}

type AcmeRetryConfig struct {
	MaxAttempts int
	Delay       time.Duration
}

func ACMEOperation(ctx context.Context, cfg AcmeRetryConfig, operation func() error) error {
	rc := DefaultConfig()
	rc.MaxAttempts = cfg.MaxAttempts
	if cfg.Delay > 0 {
		rc.InitialDelay = cfg.Delay
	}
	rc.RetryableErrors = func(err error) bool {
		var appErr *errors.AppError
		if ok := asAppError(err, &appErr); ok {
			return appErr.Code == errors.ErrACME || appErr.Retryable
		}
		return false
	}
	return Do(ctx, rc, operation)
}

func WithDefaultDBRetry(operation func() error) error {
	ctx := context.Background()
	return DatabaseOperation(ctx, DatabaseRetryConfig{MaxAttempts: 3, Delay: 200 * time.Millisecond}, operation)
}

func WithDefaultDockerRetry(operation func() error) error {
	ctx := context.Background()
	return DockerOperation(ctx, DockerRetryConfig{MaxAttempts: 3, Delay: 500 * time.Millisecond}, operation)
}
