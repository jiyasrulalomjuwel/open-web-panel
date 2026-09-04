package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// parseK8sCPU normalizes "451m"/"1916Mi.."/"2"/"1500000n" to millicores + display.
func parseK8sCPU(s string) (float64, string) {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "n") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "n"), 64)
		return v / 1e6, fmt.Sprintf("%.0fm", v/1e6)
	}
	if strings.HasSuffix(s, "u") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "u"), 64)
		return v / 1e3, fmt.Sprintf("%.0fm", v/1e3)
	}
	if strings.HasSuffix(s, "m") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "m"), 64)
		return v, s
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v * 1000, s
}

// parseK8sMem normalizes "3156Mi"/"4000524Ki"/"8Gi" to bytes + display.
func parseK8sMem(s string) (float64, string) {
	s = strings.TrimSpace(s)
	mult := 1.0
	num := s
	for _, suf := range []struct {
		s string
		m float64
	}{{"Ki", 1024}, {"Mi", 1024 * 1024}, {"Gi", 1024 * 1024 * 1024}, {"K", 1000}, {"M", 1000 * 1000}, {"G", 1000 * 1000 * 1000}} {
		if strings.HasSuffix(s, suf.s) {
			mult = suf.m
			num = strings.TrimSuffix(s, suf.s)
			break
		}
	}
	v, _ := strconv.ParseFloat(num, 64)
	return v * mult, s
}

func fmtBytesShort(b float64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.1f Gi", b/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.0f Mi", b/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.0f Ki", b/1024)
	default:
		return fmt.Sprintf("%.0f B", b)
	}
}

// k8sUsageMap gathers live usage per node: metrics API for CPU/memory plus
// kubelet stats summary for disk + pod counts. Best effort per node.
func k8sUsageMap(staged string) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	run := func(args ...string) []byte {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		full := append([]string{"--kubeconfig", staged}, args...)
		data, err := exec.CommandContext(ctx, "kubectl", full...).Output()
		if err != nil {
			return nil
		}
		return data
	}
	// Node names for the summary loop.
	names := []string{}
	if data := run("get", "nodes", "-o", "jsonpath={range .items[*]}{.metadata.name} {end}"); data != nil {
		names = strings.Fields(string(data))
	}
	// CPU/memory usage from metrics.k8s.io.
	usage := map[string]struct{ cpu, mem string }{}
	if data := run("get", "--raw", "/apis/metrics.k8s.io/v1beta1/nodes"); data != nil {
		var m struct {
			Items []struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Usage map[string]string `json:"usage"`
			} `json:"items"`
		}
		if json.Unmarshal(data, &m) == nil {
			for _, it := range m.Items {
				usage[it.Metadata.Name] = struct{ cpu, mem string }{it.Usage["cpu"], it.Usage["memory"]}
			}
		}
	}
	// Capacity for percentages + disk/pods from kubelet summaries.
	caps := map[string]struct{ cpu, mem string }{}
	if data := run("get", "nodes", "-o", "json"); data != nil {
		var l struct {
			Items []struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Status struct {
					Capacity map[string]string `json:"capacity"`
				} `json:"status"`
			} `json:"items"`
		}
		if json.Unmarshal(data, &l) == nil {
			for _, it := range l.Items {
				caps[it.Metadata.Name] = struct{ cpu, mem string }{it.Status.Capacity["cpu"], it.Status.Capacity["memory"]}
			}
		}
	}
	for _, name := range names {
		row := map[string]interface{}{}
		cpuM, cpuS := parseK8sCPU(usage[name].cpu)
		capM, _ := parseK8sCPU(caps[name].cpu)
		if capM > 0 && cpuS != "" {
			row["cpu"] = cpuS
			row["cpu_pct"] = round1(cpuM / capM * 100)
		}
		memB, memS := parseK8sMem(usage[name].mem)
		capB, _ := parseK8sMem(caps[name].mem)
		if capB > 0 && memS != "" {
			row["memory"] = memS
			row["memory_pct"] = round1(memB / capB * 100)
		}
		if data := run("get", "--raw", "/api/v1/nodes/"+name+"/proxy/stats/summary"); data != nil {
			var s struct {
				Node struct {
					Fs struct {
						UsedBytes      float64 `json:"usedBytes"`
						AvailableBytes float64 `json:"availableBytes"`
					} `json:"fs"`
				} `json:"node"`
				Pods []struct{} `json:"pods"`
			}
			if json.Unmarshal(data, &s) == nil {
				total := s.Node.Fs.UsedBytes + s.Node.Fs.AvailableBytes
				if total > 0 {
					row["disk_used"] = fmtBytesShort(s.Node.Fs.UsedBytes)
					row["disk_pct"] = round1(s.Node.Fs.UsedBytes / total * 100)
				}
				row["pods"] = len(s.Pods)
			}
		}
		out[name] = row
	}
	return out
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}

// snapshotNodeMetrics writes one metrics row per node (roles + capacity from
// the nodes list, live numbers from usage). Runs on a ticker; UI reads the DB.
func snapshotNodeMetrics(db *sql.DB) {
	cands := kubeconfigCandidates()
	if len(cands) == 0 {
		return
	}
	staged, err := stageKubeconfig(cands[0])
	if err != nil {
		return
	}
	defer os.Remove(staged)
	run := func(args ...string) []byte {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		full := append([]string{"--kubeconfig", staged}, args...)
		data, err := exec.CommandContext(ctx, "kubectl", full...).Output()
		if err != nil {
			return nil
		}
		return data
	}
	data := run("get", "nodes", "-o", "json")
	if data == nil {
		return
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Capacity map[string]string `json:"capacity"`
			} `json:"status"`
		} `json:"items"`
	}
	if json.Unmarshal(data, &list) != nil {
		return
	}
	usage := k8sUsageMap(staged)
	for _, n := range list.Items {
		role := "worker"
		if _, ok := n.Metadata.Labels["node-role.kubernetes.io/control-plane"]; ok {
			role = "master"
		} else if _, ok := n.Metadata.Labels["node-role.kubernetes.io/master"]; ok {
			role = "master"
		}
		u := usage[n.Metadata.Name]
		str := func(k string) string {
			if s, ok := u[k].(string); ok {
				return s
			}
			return ""
		}
		num := func(k string) float64 {
			if f, ok := u[k].(float64); ok {
				return f
			}
			return 0
		}
		pods := 0
		if p, ok := u["pods"].(int); ok {
			pods = p
		}
		podsCap := 0
		fmt.Sscan(n.Status.Capacity["pods"], &podsCap)
		db.Exec(`INSERT INTO k8s_node_metrics
			(node, role, cpu_used, cpu_pct, mem_used, mem_pct, disk_used, disk_pct, pods, pods_cap)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.Metadata.Name, role, str("cpu"), num("cpu_pct"), str("memory"), num("memory_pct"),
			str("disk_used"), num("disk_pct"), pods, podsCap)
	}
	db.Exec(`DELETE FROM k8s_node_metrics WHERE created_at < datetime('now', '-7 days')`)
}

// startK8sMetricsCollector snapshots cluster usage every 30 seconds.
func startK8sMetricsCollector(db *sql.DB) {
	go func() {
		snapshotNodeMetrics(db)
		for range time.Tick(30 * time.Second) {
			snapshotNodeMetrics(db)
		}
	}()
}

func latestMetricsRows(db *sql.DB) []map[string]interface{} {
	rows, err := db.Query(`SELECT node, role, cpu_used, cpu_pct, mem_used, mem_pct,
		disk_used, disk_pct, pods, pods_cap, MAX(created_at) FROM k8s_node_metrics
		GROUP BY node ORDER BY node`)
	if err != nil {
		return []map[string]interface{}{}
	}
	defer rows.Close()
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		var node, role, cpuUsed, memUsed, diskUsed, at string
		var cpuPct, memPct, diskPct float64
		var pods, podsCap int
		if err := rows.Scan(&node, &role, &cpuUsed, &cpuPct, &memUsed, &memPct,
			&diskUsed, &diskPct, &pods, &podsCap, &at); err != nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"node": node, "role": role, "cpu_used": cpuUsed, "cpu_pct": cpuPct,
			"mem_used": memUsed, "mem_pct": memPct, "disk_used": diskUsed, "disk_pct": diskPct,
			"pods": pods, "pods_cap": podsCap, "at": at,
		})
	}
	return out
}

func k8sNodesRoutes(r chi.Router, db *sql.DB) {
	// Cluster totals + latest per-node snapshot, straight from SQLite.
	r.Get("/k8s/metrics/summary", func(w http.ResponseWriter, r *http.Request) {
		rows := latestMetricsRows(db)
		totals := map[string]interface{}{"nodes": len(rows), "pods": 0}
		for _, m := range rows {
			if p, ok := m["pods"].(int); ok {
				totals["pods"] = totals["pods"].(int) + p
			}
		}
		jsonResp(w, 200, map[string]interface{}{"nodes": rows, "totals": totals})
	})

	// Drain a node (evict workloads) — first half of safe removal.
	r.Post("/k8s/nodes/{name}/drain", func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if !validK8sNodeName(name) {
			jsonError(w, 400, "invalid node name")
			return
		}
		out, err := kubectlRun([]string{"drain", name,
			"--ignore-daemonsets", "--delete-emptydir-data", "--timeout=300s"}, "", 330*time.Second)
		if err != nil {
			jsonError(w, 500, "drain failed: "+err.Error())
			return
		}
		auditLog(db, r, "k8s.node_drain", map[string]interface{}{"node": name})
		jsonResp(w, 200, map[string]string{"status": "drained", "detail": lastLines(out, 5)})
	})

	// Remove a node: drains first, refuses the last control-plane.
	r.Delete("/k8s/nodes/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if !validK8sNodeName(name) {
			jsonError(w, 400, "invalid node name")
			return
		}
		out, err := kubectlRun([]string{"get", "nodes", "-o", "json"}, "", 20*time.Second)
		if err != nil {
			jsonError(w, 500, "cannot list nodes: "+err.Error())
			return
		}
		var list struct {
			Items []struct {
				Metadata struct {
					Name   string            `json:"name"`
					Labels map[string]string `json:"labels"`
				} `json:"metadata"`
			} `json:"items"`
		}
		if err := json.Unmarshal([]byte(out), &list); err != nil {
			jsonError(w, 500, "cannot parse nodes")
			return
		}
		found := false
		masters := 0
		isMaster := false
		for _, n := range list.Items {
			_, cp := n.Metadata.Labels["node-role.kubernetes.io/control-plane"]
			_, m := n.Metadata.Labels["node-role.kubernetes.io/master"]
			if cp || m {
				masters++
				if n.Metadata.Name == name {
					isMaster = true
				}
			}
			if n.Metadata.Name == name {
				found = true
			}
		}
		if !found {
			jsonError(w, 404, "node not found")
			return
		}
		if isMaster && masters <= 1 {
			jsonError(w, 400, "refusing to remove the last control-plane node")
			return
		}
		if _, err := kubectlRun([]string{"drain", name,
			"--ignore-daemonsets", "--delete-emptydir-data", "--timeout=300s"}, "", 330*time.Second); err != nil {
			jsonError(w, 500, "drain failed, node kept: "+err.Error())
			return
		}
		if _, err := kubectlRun([]string{"delete", "node", name}, "", 60*time.Second); err != nil {
			jsonError(w, 500, "delete failed: "+err.Error())
			return
		}
		db.Exec(`DELETE FROM k8s_node_metrics WHERE node = ?`, name)
		db.Exec(`DELETE FROM k8s_diagnostics WHERE node = ?`, name)
		auditLog(db, r, "k8s.node_delete", map[string]interface{}{"node": name})
		jsonResp(w, 200, map[string]string{"status": "removed"})
	})
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) <= n {
		return strings.TrimSpace(s)
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
