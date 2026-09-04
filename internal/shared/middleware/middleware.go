package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/openwebcpanel/openwebcpanel/internal/shared/auth"
	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
)

func GetClaims(r *http.Request) *auth.Claims {
	claims, _ := r.Context().Value(ClaimsKey).(*auth.Claims)
	return claims
}

func AuthMiddleware(jwtManager *auth.JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				WriteError(w, r, apperrors.Unauthorized("missing authorization header"))
				return
			}
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				WriteError(w, r, apperrors.Unauthorized("invalid authorization format, expected 'Bearer <token>'"))
				return
			}
			claims, err := jwtManager.ValidateToken(parts[1])
			if err != nil {
				errMsg := err.Error()
				if strings.Contains(errMsg, "expired") {
					WriteError(w, r, apperrors.TokenExpired())
				} else {
					WriteError(w, r, apperrors.TokenInvalid(errMsg))
				}
				return
			}
			ctx := context.WithValue(r.Context(), ClaimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(ClaimsKey).(*auth.Claims)
			if !ok || claims == nil {
				WriteError(w, r, apperrors.Unauthorized("authentication required"))
				return
			}
			for _, role := range roles {
				if claims.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}
			WriteError(w, r, apperrors.Forbidden("insufficient permissions"))
		})
	}
}

func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(ClaimsKey).(*auth.Claims)
			if !ok || claims == nil {
				WriteError(w, r, apperrors.Unauthorized("authentication required"))
				return
			}
			if claims.Scope != scope {
				WriteError(w, r, apperrors.Forbidden("invalid scope"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), interest-cohort=()")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func BodyLimit(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

func CSPPolicy(policy string) func(http.Handler) http.Handler {
	if policy == "" {
		return func(next http.Handler) http.Handler {
			return next
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Security-Policy", policy)
			next.ServeHTTP(w, r)
		})
	}
}

func HSTSMiddleware(maxAge int, includeSubdomains bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			val := fmt.Sprintf("max-age=%d", maxAge)
			if includeSubdomains {
				val += "; includeSubDomains"
			}
			val += "; preload"
			w.Header().Set("Strict-Transport-Security", val)
			next.ServeHTTP(w, r)
		})
	}
}
