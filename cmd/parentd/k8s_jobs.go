package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	depsNamespace = "owp-system"
	depsImage     = "ubuntu:24.04"
	depsDeadline  = 1800 // 30 min per attempt
	depsBackoff   = 3    // automatic retries, then manual Retry in UI
)

var k8sNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

func validK8sNodeName(s string) bool {
	return s != "" && len(s) <= 253 && k8sNameRe.MatchString(strings.ToLower(s))
}

// kubectlRun executes kubectl with the staged admin kubeconfig.
func kubectlRun(args []string, stdin string, timeout time.Duration) (string, error) {
	cands := kubeconfigCandidates()
	if len(cands) == 0 {
		return "", fmt.Errorf("no kubeconfig found")
	}
	staged, err := stageKubeconfig(cands[0])
	if err != nil {
		return "", err
	}
	defer os.Remove(staged)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", staged}, args...)...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func depsScript() (string, error) {
	for _, p := range []string{
		"/usr/local/share/openwebpanel/install-worker-deps.sh",
		"deploy/k3s/install-worker-deps.sh",
	} {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("worker deps script not found in image")
}

func depsJobName(node string) string {
	s := strings.ToLower(node)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	name := "owp-deps-" + strings.Trim(b.String(), "-")
	if len(name) > 50 {
		name = name[:50]
	}
	return name
}

func depsJobYAML(jobName, node string) string {
	return fmt.Sprintf(`apiVersion: batch/v1
kind: Job
metadata:
  name: %s
  namespace: %s
  labels:
    owp-kind: deps
    owp-node: %s
spec:
  backoffLimit: %d
  activeDeadlineSeconds: %d
  template:
    metadata:
      labels:
        owp-kind: deps
        owp-node: %s
    spec:
      nodeName: %s
      restartPolicy: Never
      hostPID: true
      containers:
      - name: installer
        image: %s
        command: ["bash", "-c", "cp /etc/resolv.conf /host/tmp/owp-pod-resolv.conf 2>/dev/null; cp /opt/owp/install-worker-deps.sh /host/tmp/owp-deps.sh && chroot /host bash /tmp/owp-deps.sh"]
        securityContext:
          privileged: true
        volumeMounts:
        - { name: host-root, mountPath: /host }
        - { name: script, mountPath: /opt/owp }
      volumes:
      - name: host-root
        hostPath: { path: /, type: Directory }
      - name: script
        configMap: { name: owp-worker-deps }
`, jobName, depsNamespace, node, depsBackoff, depsDeadline, node, node, depsImage)
}

// depsStates derives per-node install state from installer Jobs with a single
// API call. Latest job per node wins.
func depsStates() map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	raw, err := kubectlRun([]string{"-n", depsNamespace, "get", "jobs", "-l", "owp-kind=deps", "-o", "json"}, "", 15*time.Second)
	if err != nil {
		return out
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name              string            `json:"name"`
				Labels            map[string]string `json:"labels"`
				CreationTimestamp string            `json:"creationTimestamp"`
			} `json:"metadata"`
			Status struct {
				Active    int `json:"active"`
				Succeeded int `json:"succeeded"`
				Failed    int `json:"failed"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return out
	}
	type jd struct {
		at              string
		active, succ, fail int
		job             string
	}
	latest := map[string]jd{}
	for _, j := range list.Items {
		node := j.Metadata.Labels["owp-node"]
		if node == "" {
			continue
		}
		if cur, ok := latest[node]; !ok || j.Metadata.CreationTimestamp > cur.at {
			latest[node] = jd{j.Metadata.CreationTimestamp, j.Status.Active, j.Status.Succeeded, j.Status.Failed, j.Metadata.Name}
		}
	}
	for node, j := range latest {
		st := map[string]interface{}{"state": "installing", "job": j.job}
		switch {
		case j.active > 0:
			st["state"] = "installing"
		case j.succ > 0:
			st["state"] = "ready"
		case j.fail > depsBackoff:
			st["state"] = "failed"
			st["detail"] = fmt.Sprintf("installer failed %d times — open logs, then Retry", j.fail)
		default:
			st["state"] = "installing"
		}
		out[node] = st
	}
	return out
}

// depsState is the single-node convenience wrapper.
func depsState(node string) map[string]interface{} {
	if st, ok := depsStates()[node]; ok {
		return st
	}
	return map[string]interface{}{"state": "missing"}
}

func k8sDepsRoutes(r chi.Router, db *sql.DB) {
	// Trigger (or re-trigger) dependency install on a worker.
	r.Post("/k8s/workers/{name}/install-deps", func(w http.ResponseWriter, r *http.Request) {
		node := chi.URLParam(r, "name")
		if !validK8sNodeName(node) {
			jsonError(w, 400, "invalid node name")
			return
		}
		script, err := depsScript()
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		// Namespace must exist before the Job/ConfigMap apply.
		_, _ = kubectlRun([]string{"create", "namespace", depsNamespace}, "", 15*time.Second)
		base := depsJobName(node)
		// Fresh attempt each trigger: remove any previous job, then apply.
		_, _ = kubectlRun([]string{"-n", depsNamespace, "delete", "job", "-l", "owp-node=" + node}, "", 30*time.Second)
		cm := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: owp-worker-deps
  namespace: %s
data:
  install-worker-deps.sh: |
%s
---
`, depsNamespace, indentScript(script))
		jobName := fmt.Sprintf("%s-%d", base, time.Now().Unix())
		manifest := cm + depsJobYAML(jobName, node)
		if _, err := kubectlRun([]string{"-n", depsNamespace, "apply", "-f", "-"}, manifest, 30*time.Second); err != nil {
			jsonError(w, 500, "failed to start installer job: "+err.Error())
			return
		}
		auditLog(db, r, "k8s.worker_install_deps", map[string]interface{}{"node": node, "job": jobName})
		jsonResp(w, 202, map[string]interface{}{"status": "started", "job": jobName})
	})

	// Log tail (poll fallback).
	r.Get("/k8s/workers/{name}/install-log", func(w http.ResponseWriter, r *http.Request) {
		node := chi.URLParam(r, "name")
		if !validK8sNodeName(node) {
			jsonError(w, 400, "invalid node name")
			return
		}
		tail := r.URL.Query().Get("tail")
		if tail == "" {
			tail = "200"
		}
		out, err := kubectlRun([]string{"-n", depsNamespace, "logs", "-l", "owp-node=" + node,
			"--tail", tail, "--prefix"}, "", 20*time.Second)
		if err != nil {
			jsonResp(w, 200, map[string]interface{}{"lines": "", "error": err.Error()})
			return
		}
		jsonResp(w, 200, map[string]interface{}{"lines": out})
	})

	// Live log stream (server-sent events).
	r.Get("/k8s/workers/{name}/install-log/stream", func(w http.ResponseWriter, r *http.Request) {
		node := chi.URLParam(r, "name")
		if !validK8sNodeName(node) {
			jsonError(w, 400, "invalid node name")
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			jsonError(w, 500, "streaming unsupported")
			return
		}
		cands := kubeconfigCandidates()
		if len(cands) == 0 {
			jsonError(w, 500, "no kubeconfig found")
			return
		}
		staged, err := stageKubeconfig(cands[0])
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		defer os.Remove(staged)
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "kubectl", "--kubeconfig", staged,
			"-n", depsNamespace, "logs", "-l", "owp-node="+node, "-f", "--tail", "50")
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, 4096)
		for {
			n, err := pipe.Read(buf)
			if n > 0 {
				fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(string(buf[:n]), "\n", "\ndata: "))
				flusher.Flush()
			}
			if err != nil {
				break
			}
		}
		cmd.Wait()
		fmt.Fprintf(w, "event: done\ndata: end\n\n")
		flusher.Flush()
	})
}

func indentScript(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		b.WriteString("    ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}
