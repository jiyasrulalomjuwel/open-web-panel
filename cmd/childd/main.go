package main

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	apperrors "github.com/openwebcpanel/openwebcpanel/internal/shared/errors"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/filesystem"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/logging"
	"github.com/openwebcpanel/openwebcpanel/internal/shared/middleware"
)

func jsonResp(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// requireSharedToken protects childd's file daemon. childd is a READ-ONLY file
// lister that may run inside an account container or per-account on the host.
// It must never be reachable unauthenticated from other containers or network
// peers, so every non-health endpoint demands an Authorization bearer token
// matching OWP_CHILD_SHARED_SECRET. When the secret is not configured the
// daemon refuses to serve the file list at all (defense in depth).
func requireSharedToken(secret string) func(http.Handler) http.Handler {
	if secret == "" {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				middleware.WriteError(w, r, apperrors.Forbidden("daemon not configured for remote access"))
			})
		}
	}
	want := []byte(secret)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(auth, prefix) {
				middleware.WriteError(w, r, apperrors.Unauthorized(""))
				return
			}
			got := []byte(strings.TrimSpace(auth[len(prefix):]))
			if subtle.ConstantTimeCompare(want, got) != 1 {
				middleware.WriteError(w, r, apperrors.Unauthorized(""))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func main() {
	homeDir := os.Getenv("OWP_HOME_DIR")
	if homeDir == "" {
		homeDir = "/tmp/owp-child-test"
	}
	os.MkdirAll(homeDir, 0755)

	r := chi.NewRouter()
	r.Use(chimw.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
	}))

	logger := logging.NewDefault("childd")

	sharedSecret := os.Getenv("OWP_CHILD_SHARED_SECRET")

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, 200, map[string]string{"status": "ok", "home": homeDir})
	})

	r.Route("/api/v1/files", func(r chi.Router) {
		r.Use(requireSharedToken(sharedSecret))
		r.Get("/list", func(w http.ResponseWriter, r *http.Request) {
			userPath := r.URL.Query().Get("path")
			if userPath == "" {
				userPath = "/"
			}
			safePath, err := filesystem.SafePath(homeDir, userPath)
			if err != nil {
				middleware.WriteError(w, r, apperrors.Forbidden(err.Error()))
				return
			}
			entries, err := os.ReadDir(safePath)
			if err != nil {
				middleware.WriteError(w, r, apperrors.NotFound("path", userPath))
				return
			}
			type FileEntry struct {
				Name    string `json:"name"`
				Type    string `json:"type"`
				Size    string `json:"size"`
				ModTime string `json:"mod_time"`
				Perm    string `json:"perm"`
			}
			var res []FileEntry
			for _, e := range entries {
				info, err := e.Info()
				if err != nil {
					logger.Errorf("failed to stat entry %q: %v", e.Name(), err)
					continue
				}
				typ := "file"
				if e.IsDir() {
					typ = "dir"
				}
				sz := ""
				if !e.IsDir() {
					sz = filesystem.HumanSize(info.Size())
				}
				res = append(res, FileEntry{
					Name:    e.Name(),
					Type:    typ,
					Size:    sz,
					ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
					Perm:    info.Mode().String(),
				})
			}
			jsonResp(w, 200, map[string]interface{}{"path": userPath, "entries": res})
		})
	})

	listenAddr := os.Getenv("OWP_CHILD_LISTEN")
	if listenAddr == "" {
		// Default to loopback only: cross-container / network peers must not be
		// able to reach this file daemon. A reverse proxy (nginx) in the same
		// container/namespace can still forward to it.
		listenAddr = "127.0.0.1:9001"
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		os.Exit(0)
	}()

	logger.Infof("OpenWebPanel Child Daemon on %s (home: %s)", listenAddr, homeDir)
	if err := http.ListenAndServe(listenAddr, r); err != nil {
		logger.Errorf("Server error: %v", err)
	}
}
