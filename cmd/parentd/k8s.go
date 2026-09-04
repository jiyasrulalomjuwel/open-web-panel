package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// k8sCacheTTL matches the Nodes page refresh cadence: one kubectl fork per
// minute no matter how many admins poll.
const k8sCacheTTL = 60 * time.Second

var k8sCacheMu sync.Mutex
var k8sCacheAt time.Time
var k8sCachePayload map[string]interface{}

func k8sTokenFile() string {
	if p := strings.TrimSpace(os.Getenv("OWP_K3S_TOKEN_FILE")); p != "" {
		return p
	}
	return "/var/lib/rancher/k3s/server/node-token"
}

// kubeconfigCandidates returns readable kubeconfig paths in priority order.
func kubeconfigCandidates() []string {
	var out []string
	if k := strings.TrimSpace(os.Getenv("KUBECONFIG")); k != "" {
		out = append(out, k)
	}
	out = append(out, "/etc/rancher/k3s/k3s.yaml")
	if home, herr := os.UserHomeDir(); herr == nil {
		out = append(out, filepath.Join(home, ".kube", "config"))
	}
	var ok []string
	for _, p := range out {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			ok = append(ok, p)
		}
	}
	return ok
}

// k8sStatus reports K3s visibility with distinct, actionable error codes:
// no_kubeconfig (nothing installed/mounted), no_kubectl (image problem),
// unreachable (k3s down / 6443 filtered), unauthorized (bad/rotated cert),
// parse (unexpected kubectl output).
func k8sStatus() map[string]interface{} {
	k8sCacheMu.Lock()
	if k8sCachePayload != nil && time.Since(k8sCacheAt) < k8sCacheTTL {
		defer k8sCacheMu.Unlock()
		return k8sCachePayload
	}
	k8sCacheMu.Unlock()

	out := k8sStatusFresh()

	k8sCacheMu.Lock()
	k8sCachePayload = out
	k8sCacheAt = time.Now()
	k8sCacheMu.Unlock()
	return out
}

func k8sStatusFresh() map[string]interface{} {
	out := map[string]interface{}{"installed": false, "active": false, "code": "no_kubeconfig"}
	cands := kubeconfigCandidates()
	if len(cands) == 0 {
		out["error"] = "no kubeconfig found — K3s is not enabled on this host"
		return out
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		out["installed"] = true
		out["code"] = "no_kubectl"
		out["error"] = "kubeconfig present but kubectl is not installed in this image"
		return out
	}
	out["installed"] = true

	staged, err := stageKubeconfig(cands[0])
	if err != nil {
		out["code"] = "kubeconfig_invalid"
		out["error"] = err.Error()
		return out
	}
	defer os.Remove(staged)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "kubectl", "--kubeconfig", staged, "get", "nodes", "-o", "json").Output()
	if err != nil {
		out["code"] = "unreachable"
		out["error"] = "kubernetes API unreachable — is k3s running and port 6443 open?"
		if ee, ok := err.(*exec.ExitError); ok && strings.Contains(string(ee.Stderr), "Unauthorized") {
			out["code"] = "unauthorized"
			out["error"] = "API refused our credentials — kubeconfig certificate rotated? Re-mount /etc/rancher/k3s/k3s.yaml."
		}
		return out
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
				NodeInfo struct {
					KubeletVersion string `json:"kubeletVersion"`
				} `json:"nodeInfo"`
				Capacity map[string]string `json:"capacity"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		out["code"] = "parse"
		out["error"] = "cannot parse kubectl output"
		return out
	}
	serverMinor := k8sServerMinor(staged)
	usage := k8sUsageMap(staged)
	deps := depsStates()
	nodes := make([]map[string]interface{}, 0, len(list.Items))
	ready := 0
	for _, n := range list.Items {
		isReady := false
		for _, c := range n.Status.Conditions {
			if c.Type == "Ready" && c.Status == "True" {
				isReady = true
			}
		}
		if isReady {
			ready++
		}
		role := "worker"
		if _, ok := n.Metadata.Labels["node-role.kubernetes.io/control-plane"]; ok {
			role = "master"
		} else if _, ok := n.Metadata.Labels["node-role.kubernetes.io/master"]; ok {
			role = "master"
		}
		u := usage[n.Metadata.Name]
		dst, ok := deps[n.Metadata.Name]
		if !ok {
			dst = map[string]interface{}{"state": "missing"}
		}
		nodes = append(nodes, map[string]interface{}{
			"name":        n.Metadata.Name,
			"role":        role,
			"ready":       isReady,
			"version":     n.Status.NodeInfo.KubeletVersion,
			"version_ok":  k8sSkewOK(serverMinor, n.Status.NodeInfo.KubeletVersion),
			"cpu":         n.Status.Capacity["cpu"],
			"memory":      n.Status.Capacity["memory"],
			"cpu_usage":   u["cpu"],
			"cpu_pct":     u["cpu_pct"],
			"mem_usage":   u["memory"],
			"mem_pct":     u["memory_pct"],
			"disk_used":   u["disk_used"],
			"disk_pct":    u["disk_pct"],
			"pods":        u["pods"],
			"pods_cap":    n.Status.Capacity["pods"],
			"deps":        dst,
		})
	}
	out["active"] = ready > 0
	out["code"] = "ok"
	out["nodes"] = nodes
	out["node_count"] = len(nodes)
	out["ready_count"] = ready
	if sv := k8sServerVersion(staged); sv != "" {
		out["server_version"] = sv
	}
	return out
}

// stageKubeconfig copies the source with host-reachability fixes applied and
// returns the temp path.
func stageKubeconfig(src string) (string, error) {
	raw, err := os.ReadFile(src)
	if err != nil {
		return "", fmt.Errorf("cannot read kubeconfig: %w", err)
	}
	effective := string(raw)
	if override := strings.TrimSpace(os.Getenv("OWP_K8S_SERVER")); override != "" {
		effective = replaceKubeServer(effective, override)
	} else if isLoopbackServer(effective) {
		if gw := defaultGateway(); gw != "" {
			effective = replaceKubeServerHost(effective, gw)
		}
	}
	tmp, err := os.CreateTemp("", "owp-kube-*.yaml")
	if err != nil {
		return "", fmt.Errorf("cannot stage kubeconfig")
	}
	// os.CreateTemp already creates 0600.
	if _, err := tmp.WriteString(effective); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", fmt.Errorf("cannot stage kubeconfig")
	}
	tmp.Close()
	return tmp.Name(), nil
}

// k8sServerVersion queries the API server version (best effort, cached path).
func k8sServerVersion(staged string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, "kubectl", "--kubeconfig", staged, "version", "-o", "json").Output()
	if err != nil {
		return ""
	}
	var v struct {
		ServerVersion struct {
			GitVersion string `json:"gitVersion"`
		} `json:"serverVersion"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return ""
	}
	return v.ServerVersion.GitVersion
}

func k8sServerMinor(staged string) string {
	return k8sMinor(k8sServerVersion(staged))
}

func k8sMinor(v string) string {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return ""
	}
	return parts[0] + "." + parts[1]
}

// k8sSkewOK enforces the Kubernetes version-skew policy loosely: same major
// and at most one minor apart. Unparsable versions fail closed.
func k8sSkewOK(serverMinor, kubelet string) bool {
	if serverMinor == "" {
		return true // server unknown: don't block on policy
	}
	km := k8sMinor(kubelet)
	if km == "" {
		return false
	}
	sMaj, sMin, ok1 := k8sVerParts(serverMinor)
	kMaj, kMin, ok2 := k8sVerParts(km)
	if !ok1 || !ok2 || sMaj != kMaj {
		return false
	}
	d := sMin - kMin
	if d < 0 {
		d = -d
	}
	return d <= 1
}

func k8sVerParts(mm string) (int, int, bool) {
	p := strings.Split(mm, ".")
	if len(p) != 2 {
		return 0, 0, false
	}
	maj, err1 := strconv.Atoi(p[0])
	min, err2 := strconv.Atoi(p[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return maj, min, true
}

// k8sJoinCommand builds the worker join command from the auto-read server
// token file. The token itself is only ever returned by the explicit
// join-command endpoint (audit-logged), never in status output.
func k8sJoinCommand() (string, error) {
	raw, err := os.ReadFile(k8sTokenFile())
	if err != nil {
		return "", fmt.Errorf("server token not readable (is K3s installed here? check OWP_K3S_TOKEN_FILE)")
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", fmt.Errorf("server token file is empty")
	}
	host := strings.TrimSpace(os.Getenv("OWP_K3S_JOIN_HOST"))
	if host == "" {
		host = getSharedIP()
	}
	if host == "" || host == "127.0.0.1" || strings.HasPrefix(host, "127.") {
		return "", fmt.Errorf("cannot determine a reachable master address (set OWP_K3S_JOIN_HOST)")
	}
	return fmt.Sprintf("curl -sfL https://get.k3s.io | K3S_URL=https://%s:6443 K3S_TOKEN=%s sh -", host, token), nil
}

func replaceKubeServer(cfg, server string) string {
	lines := strings.Split(cfg, "\n")
	for i, l := range lines {
		if strings.Contains(strings.TrimSpace(l), "server:") {
			indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
			lines[i] = indent + "server: " + strings.TrimSpace(server)
			break
		}
	}
	return strings.Join(lines, "\n")
}

func replaceKubeServerHost(cfg, host string) string {
	lines := strings.Split(cfg, "\n")
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "server:") {
			continue
		}
		url := strings.TrimSpace(strings.TrimPrefix(t, "server:"))
		port := "6443"
		if h := strings.SplitN(url, "://", 2); len(h) == 2 {
			if hp := strings.SplitN(h[1], "/", 2); len(hp) >= 1 {
				if parts := strings.Split(hp[0], ":"); len(parts) == 2 && parts[1] != "" {
					port = parts[1]
				}
			}
		}
		indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		lines[i] = fmt.Sprintf("%sserver: https://%s:%s", indent, host, port)
		break
	}
	return strings.Join(lines, "\n")
}

func isLoopbackServer(cfg string) bool {
	for _, l := range strings.Split(cfg, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "server:") && (strings.Contains(t, "127.0.0.1") || strings.Contains(t, "localhost")) {
			return true
		}
	}
	return false
}

// defaultGateway reads the container/host default route from /proc/net/route
// (gateway stored hex little-endian).
func defaultGateway() string {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 3 || f[1] != "00000000" {
			continue
		}
		u, err := strconv.ParseUint(f[2], 16, 32)
		if err != nil || u == 0 {
			continue
		}
		return fmt.Sprintf("%d.%d.%d.%d", byte(u), byte(u>>8), byte(u>>16), byte(u>>24))
	}
	return ""
}

func k8sRoutes(r chi.Router, db *sql.DB) {
	r.Get("/k8s/status", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, 200, k8sStatus())
	})
	r.Post("/k8s/join-command", func(w http.ResponseWriter, r *http.Request) {
		cmd, err := k8sJoinCommand()
		if err != nil {
			jsonError(w, 404, err.Error())
			return
		}
		auditLog(db, r, "k8s.join_command", map[string]interface{}{})
		jsonResp(w, 200, map[string]string{"command": cmd})
	})
}
