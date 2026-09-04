package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	diagNamespace = "owp-system"
	diagImage     = "ubuntu:24.04"
	diagDeadline  = 600 // 10 min per run
	diagCacheNote = ""
)

var _ = diagCacheNote

// diagScript loads the worker diagnostics script baked into the image.
func diagScript() (string, error) {
	for _, p := range []string{
		"/usr/local/share/openwebpanel/worker-diagnose.sh",
		"deploy/k3s/worker-diagnose.sh",
	} {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			return string(b), nil
		}
	}
	return "", fmt.Errorf("worker diagnostics script not found in image")
}

func diagJobName(node, mode string) string {
	s := strings.ToLower(node)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	name := "owp-diag-" + strings.Trim(b.String(), "-")
	if len(name) > 45 {
		name = name[:45]
	}
	return fmt.Sprintf("%s-%s-%d", name, mode, time.Now().Unix())
}

func diagJobYAML(jobName, node, role string) string {
	return fmt.Sprintf(`apiVersion: batch/v1
kind: Job
metadata:
  name: %s
  namespace: %s
  labels:
    owp-kind: diag
    owp-node: %s
spec:
  backoffLimit: 0
  activeDeadlineSeconds: %d
  template:
    metadata:
      labels:
        owp-kind: diag
        owp-node: %s
    spec:
      nodeName: %s
      restartPolicy: Never
      hostPID: true
      hostNetwork: true
      containers:
      - name: diag
        image: %s
        env:
        - { name: OWP_DIAG_ROLE, value: %s }
        command: ["bash", "-c", "cp /etc/resolv.conf /host/tmp/owp-pod-resolv.conf 2>/dev/null; cp /opt/owp/worker-diagnose.sh /host/tmp/owp-diag.sh && chroot /host bash /tmp/owp-diag.sh \"$OWP_DIAG_MODE\" \"$OWP_DIAG_ROLE\""]
        volumeMounts:
        - { name: host-root, mountPath: /host }
        - { name: script, mountPath: /opt/owp }
      volumes:
      - name: host-root
        hostPath: { path: /, type: Directory }
      - name: script
        configMap: { name: owp-diag }
`, jobName, diagNamespace, node, diagDeadline, node, node, diagImage, role)
}

type diagResult struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok|fail|skip (+ fixed|failed for FIX lines)
	Detail string `json:"detail"`
	Kind   string `json:"kind"` // check|fix
}

// parseDiagLines extracts CHECK/FIX protocol lines, ignoring apt noise.
func parseDiagLines(logs string) []diagResult {
	var out []diagResult
	for _, line := range strings.Split(logs, "\n") {
		line = strings.TrimSpace(line)
		// kubectl --prefix prepends "[pod/...]" — strip it.
		if strings.HasPrefix(line, "[pod/") {
			if i := strings.Index(line, "] "); i >= 0 {
				line = strings.TrimSpace(line[i+2:])
			}
		}
		f := strings.Fields(line)
		if len(f) < 3 || (f[0] != "CHECK" && f[0] != "FIX") {
			continue
		}
		detail := ""
		if len(f) > 3 {
			detail = strings.Join(f[3:], " ")
		}
		out = append(out, diagResult{
			Kind:   strings.ToLower(f[0]),
			Name:   f[1],
			Status: strings.ToLower(f[2]),
			Detail: detail,
		})
	}
	return out
}

func nodeRole(node string) string {
	out, err := kubectlRun([]string{"get", "node", node, "-o", "jsonpath={.metadata.labels}"}, "", 15*time.Second)
	if err != nil {
		return "worker"
	}
	if strings.Contains(out, "node-role.kubernetes.io/control-plane") || strings.Contains(out, "node-role.kubernetes.io/master") {
		return "master"
	}
	return "worker"
}

// runDiagJob executes one diagnostics pass (mode check|fix) on a node and
// waits up to ~150s for pod logs. Returns parsed results.
func runDiagJob(node, mode string) ([]diagResult, error) {
	if !validK8sNodeName(node) {
		return nil, fmt.Errorf("invalid node name")
	}
	script, err := diagScript()
	if err != nil {
		return nil, err
	}
	if _, err := kubectlRun([]string{"create", "namespace", diagNamespace}, "", 15*time.Second); err != nil {
		// already exists most of the time; only log real failures implicitly
		_ = err
	}
	role := nodeRole(node)
	cm := fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: owp-diag
  namespace: %s
data:
  worker-diagnose.sh: |
%s
---
`, diagNamespace, indentScript(script))
	jobName := diagJobName(node, mode)
	// Pass mode+role via env patch on the manifest (base template uses env).
	manifest := strings.Replace(cm+diagJobYAML(jobName, node, role),
		`"$OWP_DIAG_MODE" "$OWP_DIAG_ROLE"`,
		fmt.Sprintf(`"%s" "%s"`, mode, role), 1)
	if _, err := kubectlRun([]string{"-n", diagNamespace, "apply", "-f", "-"}, manifest, 30*time.Second); err != nil {
		return nil, fmt.Errorf("failed to start diagnostics job: %w", err)
	}
	// Poll THIS job's pods only (built-in job-name label): older runs for the
	// same node must never satisfy a fresh check.
	sel := "job-name=" + jobName
	deadline := time.Now().Add(150 * time.Second)
	for {
		out, err := kubectlRun([]string{"-n", diagNamespace, "logs", "-l", sel,
			"--tail", "200"}, "", 20*time.Second)
		if err == nil {
			if res := parseDiagLines(out); len(res) > 0 {
				pruneDiagJobs(node, jobName)
				return res, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("diagnostics timed out waiting for logs")
		}
		time.Sleep(5 * time.Second)
	}
}

// latestDiagJobName returns the newest diagnostics job for a node so log
// readers never mix in stale runs.
func latestDiagJobName(node string) string {
	out, err := kubectlRun([]string{"-n", diagNamespace, "get", "jobs", "-l", "owp-node=" + node,
		"--sort-by=.metadata.creationTimestamp", "-o", "jsonpath={.items[-1:].metadata.name}"}, "", 15*time.Second)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// pruneDiagJobs deletes older diagnostics jobs for a node, keeping history
// bounded (latest run retained). Scoped to owp-kind=diag so dependency
// installer jobs are never touched.
func pruneDiagJobs(node, keep string) {
	out, err := kubectlRun([]string{"-n", diagNamespace, "get", "jobs", "-l", "owp-kind=diag,owp-node=" + node,
		"-o", "jsonpath={range .items[*]}{.metadata.name} {.metadata.creationTimestamp}{\"\\n\"}{end}"}, "", 15*time.Second)
	if err != nil {
		return
	}
	type item struct {
		name, at string
	}
	var items []item
	var newest string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 {
			continue
		}
		items = append(items, item{f[0], f[1]})
		if f[1] > newest {
			newest = f[1]
		}
	}
	for _, it := range items {
		if it.name == keep || it.at == newest {
			continue
		}
		_, _ = kubectlRun([]string{"-n", diagNamespace, "delete", "job", it.name}, "", 15*time.Second)
	}
}

func storeDiagResults(db *sql.DB, node string, results []diagResult) {
	for _, r := range results {
		if r.Kind != "check" {
			continue
		}
		db.Exec(`INSERT INTO k8s_diagnostics (node, check_name, status, detail) VALUES (?, ?, ?, ?)`,
			node, r.Name, r.Status, r.Detail)
	}
	db.Exec(`DELETE FROM k8s_diagnostics WHERE created_at < datetime('now', '-7 days')`)
}

func k8sAutofixEnabled(db *sql.DB) bool {
	var v string
	if err := db.QueryRow("SELECT value FROM server_config WHERE key_name = 'k8s_autofix'").Scan(&v); err != nil {
		return true
	}
	return v != "0" && v != "false"
}

// checkNodeOnce runs check mode and, when fixable items fail and autofix is
// on, follows with a fix run. Returns the check results.
func checkNodeOnce(db *sql.DB, node string) []diagResult {
	results, err := runDiagJob(node, "check")
	if err != nil {
		return []diagResult{{Kind: "check", Name: "job", Status: "fail", Detail: err.Error()}}
	}
	storeDiagResults(db, node, results)
	needsFix := false
	for _, r := range results {
		if r.Kind == "check" && r.Status == "fail" {
			needsFix = true
			break
		}
	}
	if needsFix && k8sAutofixEnabled(db) {
		if fixRes, err := runDiagJob(node, "fix"); err == nil {
			storeDiagResults(db, node, fixRes)
			// Re-check to record the post-fix state.
			if re, err := runDiagJob(node, "check"); err == nil {
				storeDiagResults(db, node, re)
				return re
			}
			return fixRes
		}
	}
	return results
}

// startK8sHealthChecker runs diagnostics across Ready nodes every 5 minutes.
func startK8sHealthChecker(db *sql.DB) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			st := k8sStatusFresh()
			nodes, _ := st["nodes"].([]map[string]interface{})
			for _, n := range nodes {
				name, _ := n["name"].(string)
				ready, _ := n["ready"].(bool)
				if name == "" || !ready {
					continue
				}
				checkNodeOnce(db, name)
			}
		}
	}()
}

func latestDiagResults(db *sql.DB, node string) []map[string]interface{} {
	rows, err := db.Query(`SELECT check_name, status, detail, MAX(created_at) FROM k8s_diagnostics
		WHERE node = ? GROUP BY check_name ORDER BY check_name`, node)
	if err != nil {
		return []map[string]interface{}{}
	}
	defer rows.Close()
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		var name, status, detail, at string
		if err := rows.Scan(&name, &status, &detail, &at); err != nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"name": name, "status": status, "detail": detail, "checked_at": at,
		})
	}
	return out
}

func k8sHealthRoutes(r chi.Router, db *sql.DB) {
	r.Get("/k8s/workers/{name}/health", func(w http.ResponseWriter, r *http.Request) {
		node := chi.URLParam(r, "name")
		if !validK8sNodeName(node) {
			jsonError(w, 400, "invalid node name")
			return
		}
		jsonResp(w, 200, map[string]interface{}{"node": node, "checks": latestDiagResults(db, node)})
	})
	r.Post("/k8s/workers/{name}/health/recheck", func(w http.ResponseWriter, r *http.Request) {
		node := chi.URLParam(r, "name")
		if !validK8sNodeName(node) {
			jsonError(w, 400, "invalid node name")
			return
		}
		results, err := runDiagJob(node, "check")
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		storeDiagResults(db, node, results)
		auditLog(db, r, "k8s.worker_recheck", map[string]interface{}{"node": node})
		jsonResp(w, 200, map[string]interface{}{"node": node, "checks": results})
	})
	r.Post("/k8s/workers/{name}/health/fix", func(w http.ResponseWriter, r *http.Request) {
		node := chi.URLParam(r, "name")
		if !validK8sNodeName(node) {
			jsonError(w, 400, "invalid node name")
			return
		}
		results, err := runDiagJob(node, "fix")
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		re, err := runDiagJob(node, "check")
		if err == nil {
			storeDiagResults(db, node, re)
			results = re
		}
		auditLog(db, r, "k8s.worker_fix", map[string]interface{}{"node": node})
		jsonResp(w, 200, map[string]interface{}{"node": node, "checks": results})
	})
}
