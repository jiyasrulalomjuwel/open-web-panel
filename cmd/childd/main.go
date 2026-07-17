package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/openwebcpanel/openwebcpanel/internal/shared/filesystem"
)

func jsonResp(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	jsonResp(w, status, map[string]string{"error": msg})
}

func main() {
	homeDir := os.Getenv("OWP_HOME_DIR")
	if homeDir == "" {
		homeDir = "/tmp/owp-child-test"
	}
	os.MkdirAll(homeDir, 0755)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
	}))

	// Health
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, 200, map[string]string{"status": "ok", "home": homeDir})
	})

	// Files
	r.Get("/api/v1/files/list", func(w http.ResponseWriter, r *http.Request) {
		userPath := r.URL.Query().Get("path")
		if userPath == "" {
			userPath = "/"
		}
		safePath, err := filesystem.SafePath(homeDir, userPath)
		if err != nil {
			jsonError(w, 403, err.Error())
			return
		}
		entries, err := os.ReadDir(safePath)
		if err != nil {
			jsonError(w, 404, "not found")
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
			info, _ := e.Info()
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

	listenAddr := os.Getenv("OWP_CHILD_LISTEN")
	if listenAddr == "" {
		listenAddr = ":9001"
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		os.Exit(0)
	}()

	log.Printf("OpenWebPanel Child Daemon on %s (home: %s)", listenAddr, homeDir)
	if err := http.ListenAndServe(listenAddr, r); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
