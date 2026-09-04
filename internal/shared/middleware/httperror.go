package middleware

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
)

type ContextKey string

const RequestIDKey ContextKey = "request_id"
const ClaimsKey ContextKey = "claims"

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code    string      `json:"code"`
	Message string      `json:"message"`
	Details interface{} `json:"details,omitempty"`
	Fields  interface{} `json:"fields,omitempty"`
	RequestID string   `json:"request_id,omitempty"`
}

func WriteError(w http.ResponseWriter, r *http.Request, appErr *apperrors.AppError) {
	requestID, _ := r.Context().Value(RequestIDKey).(string)

	if requestID == "" {
		requestID = sanitizeRequestID(r.Header.Get("X-Request-ID"))
	}

	status := apperrors.HTTPStatus(appErr.Code)
	if status == 0 {
		status = http.StatusInternalServerError
	}

	resp := ErrorResponse{
		Error: ErrorBody{
			Code:      string(appErr.Code),
			Message:   appErr.Message,
			RequestID: requestID,
		},
	}

	if len(appErr.Fields) > 0 {
		resp.Error.Fields = appErr.Fields
	}

	if appErr.Details != nil && status < 500 {
		resp.Error.Details = appErr.Details
	}

	if status >= 500 {
		resp.Error.Message = "an internal error occurred"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func WriteErrorMsg(w http.ResponseWriter, r *http.Request, status int, code string, message string) {
	requestID, _ := r.Context().Value(RequestIDKey).(string)
	if requestID == "" {
		requestID = sanitizeRequestID(r.Header.Get("X-Request-ID"))
	}

	resp := ErrorResponse{
		Error: ErrorBody{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				requestID, _ := r.Context().Value(RequestIDKey).(string)
				log.Printf("[PANIC] request_id=%s panic=%v stack=%s", requestID, rec, debug.Stack())

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(ErrorResponse{
					Error: ErrorBody{
						Code:      "INTERNAL_ERROR",
						Message:   "an unexpected error occurred",
						RequestID: requestID,
					},
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// sanitizeRequestID returns an empty string if id is missing or contains
// characters outside the restricted set, to prevent log injection via
// client-supplied request IDs.
func sanitizeRequestID(id string) string {
	if id != "" && validRequestID.MatchString(id) {
		return id
	}
	return ""
}

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitizeRequestID(r.Header.Get("X-Request-ID"))
		if id == "" {
			id = uuid.New().String()
		}
		ctx := context.WithValue(r.Context(), RequestIDKey, id)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
}

func RequestLogger(logger *logging.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID, _ := r.Context().Value(RequestIDKey).(string)

			rLogger := logger.WithRequestID(requestID)
			ctx := logging.WithLogger(r.Context(), rLogger)
			r = r.WithContext(ctx)

			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)
			fields := logging.Fields{
				"method":   r.Method,
				"path":     r.URL.Path,
				"query":    r.URL.RawQuery,
				"status":   wrapped.statusCode,
				"duration": duration.String(),
				"bytes":    wrapped.bytesWritten,
				"remote":   r.RemoteAddr,
				"ua":       r.UserAgent(),
			}

			if wrapped.statusCode >= 500 {
				rLogger.Error("request failed", fields)
			} else if wrapped.statusCode >= 400 {
				rLogger.Warn("request warning", fields)
			} else {
				rLogger.Info("request completed", fields)
			}
		})
	}
}

type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    int
	window   time.Duration
}

type visitor struct {
	count    int
	expireAt time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		limit:    limit,
		window:   window,
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, v := range rl.visitors {
			if now.After(v.expireAt) {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Key on the real remote IP, without the source port, so that all
		// connections coming from one client share a single bucket. The
		// client-supplied X-Forwarded-For header is deliberately ignored:
		// this package has no trusted-proxy configuration, so trusting it
		// would let a client spoof its identity and never hit the limit.
		ip := r.RemoteAddr
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			ip = host
		}

		rl.mu.Lock()
		v, exists := rl.visitors[ip]
		now := time.Now()

		if !exists || now.After(v.expireAt) {
			rl.visitors[ip] = &visitor{count: 1, expireAt: now.Add(rl.window)}
			rl.mu.Unlock()
			next.ServeHTTP(w, r)
			return
		}

		v.count++
		if v.count > rl.limit {
			retryAfter := int(v.expireAt.Sub(now).Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			rl.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(ErrorResponse{
				Error: ErrorBody{
					Code:    "RATE_LIMIT",
					Message: "too many requests",
					Details: map[string]int{"retry_after_seconds": retryAfter},
				},
			})
			return
		}
		rl.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func JSONContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

func ValidateContentType(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "PATCH" {
			ct := r.Header.Get("Content-Type")
			if ct == "" || strings.HasPrefix(ct, "multipart/form-data") {
				next.ServeHTTP(w, r)
				return
			}
			if !strings.Contains(ct, "application/json") && !strings.Contains(ct, "application/x-www-form-urlencoded") {
				WriteErrorMsg(w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE",
					"content-type must be application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func CORSHandler(next http.Handler) http.Handler {
	return next
}
