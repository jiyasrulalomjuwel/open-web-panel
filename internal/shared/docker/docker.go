package docker

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type ContainerInfo struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Image           string  `json:"image"`
	Status          string  `json:"status"`
	AccountID       int     `json:"account_id"`
	Username        string  `json:"username"`
	ContainerIP     string  `json:"container_ip"`
	CPULimit        float64 `json:"cpu_limit"`
	RAMLimitMB      int     `json:"ram_limit_mb"`
	StorageLimitGB  int     `json:"storage_limit_gb"`
	CreatedAt       string  `json:"created_at"`
	CPUCoresUsage   float64 `json:"cpu_cores_usage,omitempty"`
	RAMUsageMB      int     `json:"ram_usage_mb,omitempty"`
	NetworkRXBytes  int64   `json:"network_rx_bytes,omitempty"`
	NetworkTXBytes  int64   `json:"network_tx_bytes,omitempty"`
	DiskUsageMB     int64   `json:"disk_usage_mb,omitempty"`
}

type rawInspect struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Image  string `json:"Image"`
	State  struct {
		Status string `json:"Status"`
	} `json:"State"`
	Created string `json:"Created"`
	Config  struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		Memory     int64             `json:"Memory"`
		NanoCPUs   int64             `json:"NanoCpus"`
		StorageOpt map[string]string `json:"StorageOpt"`
	} `json:"HostConfig"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

func EnsureInstalled() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return installDocker()
	}
	cmd := exec.Command("docker", "info")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker daemon not running: %v\n%s", err, string(out))
	}
	return nil
}

func installDocker() error {
	scripts := []string{
		"apt-get update -qq",
		"apt-get install -y -qq ca-certificates curl",
		"install -m 0755 -d /etc/apt/keyrings",
		"curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc",
		"chmod a+r /etc/apt/keyrings/docker.asc",
		"echo 'deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable' > /etc/apt/sources.list.d/docker.list",
		"apt-get update -qq",
		"apt-get install -y -qq docker-ce docker-ce-cli containerd.io",
		"systemctl enable docker",
		"systemctl start docker",
	}
	for _, s := range scripts {
		cmd := exec.Command("bash", "-c", s)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("docker install failed at %q: %v\n%s", s, err, string(out))
		}
	}
	return nil
}

func runCmd(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func ContainerName(accountID int, username string) string {
	return fmt.Sprintf("owp_%d_%s", accountID, username)
}

// childDaemonSecret derives a per-container shared secret for childd auth
// from the panel's JWT secret and the account id. childd is only reachable on
// the container's loopback, so this is defense-in-depth; it is not used to
// derive any access/refresh token.
func childDaemonSecret(accountID int) string {
	seed := os.Getenv("OWP_JWT_SECRET")
	if seed == "" {
		seed = "openwebpanel-childd-key-change-me"
	}
	return fmt.Sprintf("cdaemon_%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d", seed, accountID))))
}

func getImageName() string {
	img := os.Getenv("OWP_DOCKER_IMAGE")
	if img != "" {
		return img
	}
	return "openwebpanel/child:latest"
}

func EnsureImage() error {
	image := getImageName()
	out, err := runCmd("image", "inspect", image)
	if err == nil && out != "" {
		return nil
	}
	dockerfilePath := os.Getenv("OWP_DOCKERFILE")
	if dockerfilePath == "" {
		exe, _ := os.Executable()
		base := filepath.Dir(exe)
		candidates := []string{
			filepath.Join(base, "Dockerfile.child"),
			filepath.Join(base, "..", "Dockerfile.child"),
			filepath.Join(base, "..", "..", "Dockerfile.child"),
			"/opt/openwebpanel/app/Dockerfile.child",
			"/etc/openwebpanel/Dockerfile.child",
			"/usr/local/share/openwebpanel/Dockerfile.child",
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				dockerfilePath = p
				break
			}
		}
	}
	if dockerfilePath == "" {
		return fmt.Errorf("cannot find Dockerfile.child - set OWP_DOCKERFILE env var or place it next to the binary")
	}
	buildDir := filepath.Dir(dockerfilePath)
	log.Printf("[DOCKER] Building image %s from %s...", image, dockerfilePath)
	out, err = runCmd("build", "-t", image, "-f", dockerfilePath, buildDir)
	if err != nil {
		return fmt.Errorf("build image %s: %v\n%s", image, err, out)
	}
	log.Printf("[DOCKER] Image %s built successfully", image)
	return nil
}

func CreateContainer(accountID int, username, homeDir string, ramLimitMB int, cpuLimit float64, storageLimitGB int) (*ContainerInfo, error) {
	name := ContainerName(accountID, username)
	image := getImageName()

	os.MkdirAll(homeDir, 0755)
	os.MkdirAll(homeDir+"/public_html", 0755)

	args := []string{
		"run", "-d",
		"--name", name,
		"--restart", "unless-stopped",
		"--label", fmt.Sprintf("owp.account_id=%d", accountID),
		"--label", fmt.Sprintf("owp.username=%s", username),
	}

	if ramLimitMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", ramLimitMB))
		args = append(args, "--memory-swap", fmt.Sprintf("%dm", ramLimitMB))
	}
	if cpuLimit > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(cpuLimit, 'f', 2, 64))
	}
	if storageLimitGB > 0 {
		args = append(args, "--storage-opt", fmt.Sprintf("size=%dG", storageLimitGB))
	}

	args = append(args, "-v", fmt.Sprintf("%s:/home/user", homeDir))
	args = append(args, "-e", fmt.Sprintf("OWP_ACCOUNT_ID=%d", accountID))
	args = append(args, "-e", fmt.Sprintf("OWP_USERNAME=%s", username))
	args = append(args, "-e", "OWP_HOME=/home/user")
	args = append(args, "-e", "OWP_CHILD_LISTEN=127.0.0.1:9001")
	args = append(args, "-e", fmt.Sprintf("OWP_CHILD_SHARED_SECRET=%s", childDaemonSecret(accountID)))
	args = append(args, "-e", "OWP_MYSQL_HOST=172.18.0.1")
	args = append(args, "--network", "owp-network")
	args = append(args, image)

	if out, err := runCmd(args...); err != nil {
		return nil, fmt.Errorf("create container: %v\noutput: %s", err, out)
	}
	log.Printf("[DOCKER] Container %s created for account %s (ID: %d)", name, username, accountID)

	info, err := GetContainerInfo(name)
	if err != nil {
		return nil, err
	}

	containerIP, err := getContainerIP(name)
	if err == nil && containerIP != "" {
		info.ContainerIP = containerIP
	}
	return info, nil
}

func ProvisionAccount(accountID int, username, homeDir string, ramLimitMB int, cpuLimit float64) (*ContainerInfo, error) {
	if err := EnsureInstalled(); err != nil {
		return nil, fmt.Errorf("docker not available: %v", err)
	}
	if err := EnsureNetwork(); err != nil {
		log.Printf("[DOCKER] Network setup warning: %v", err)
	}
	if err := EnsureImage(); err != nil {
		return nil, fmt.Errorf("image not available: %v", err)
	}
	return CreateContainer(accountID, username, homeDir, ramLimitMB, cpuLimit, 0)
}

func GetContainerInfo(nameOrID string) (*ContainerInfo, error) {
	out, err := runCmd("inspect", nameOrID)
	if err != nil {
		return nil, fmt.Errorf("inspect container: %v", err)
	}
	var raw []rawInspect
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, fmt.Errorf("parse inspect: %v", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("container %s not found", nameOrID)
	}
	r := raw[0]
	accountID, _ := strconv.Atoi(r.Config.Labels["owp.account_id"])
	username := r.Config.Labels["owp.username"]
	var storageGB int
	if sz, ok := r.HostConfig.StorageOpt["size"]; ok {
		sz = strings.TrimSuffix(sz, "G")
		if v, err := strconv.Atoi(sz); err == nil {
			storageGB = v
		}
	}
	containerIP := ""
	if net, ok := r.NetworkSettings.Networks["owp-network"]; ok {
		containerIP = net.IPAddress
	}

	info := &ContainerInfo{
		ID:             r.ID[:12],
		Name:           strings.TrimPrefix(r.Name, "/"),
		Image:          r.Image,
		Status:         r.State.Status,
		AccountID:      accountID,
		Username:       username,
		ContainerIP:    containerIP,
		CPULimit:       float64(r.HostConfig.NanoCPUs) / 1e9,
		RAMLimitMB:     int(r.HostConfig.Memory / 1024 / 1024),
		StorageLimitGB: storageGB,
		CreatedAt:      r.Created,
	}
	if r.State.Status == "running" {
		stats, err := getContainerStats(r.ID)
		if err == nil && stats != nil {
			info.CPUCoresUsage = stats.CPUCoresUsage
			info.RAMUsageMB = stats.RAMUsageMB
			info.NetworkRXBytes = stats.NetworkRXBytes
			info.NetworkTXBytes = stats.NetworkTXBytes
		}
		du, err := getContainerDiskUsage(r.ID)
		if err == nil {
			info.DiskUsageMB = du
		}
	}
	return info, nil
}

type containerStats struct {
	CPUCoresUsage  float64
	RAMUsageMB     int
	NetworkRXBytes int64
	NetworkTXBytes int64
}

func getContainerStats(containerID string) (*containerStats, error) {
	out, err := runCmd("stats", "--no-stream", "--format", "{{json .}}", containerID)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(out, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw struct {
			CPUPerc  string `json:"CPUPerc"`
			MemUsage string `json:"MemUsage"`
			NetIO    string `json:"NetIO"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		stats := &containerStats{}
		cpuStr := strings.TrimSuffix(raw.CPUPerc, "%")
		if cpuVal, err := strconv.ParseFloat(cpuStr, 64); err == nil {
			stats.CPUCoresUsage = cpuVal / 100.0
		}
		memParts := strings.Split(raw.MemUsage, " / ")
		if len(memParts) > 0 {
			memStr := strings.TrimSpace(memParts[0])
			stats.RAMUsageMB = parseSizeToMB(memStr)
		}
		netParts := strings.Split(raw.NetIO, " / ")
		if len(netParts) >= 2 {
			stats.NetworkRXBytes = parseSizeToBytes(strings.TrimSpace(netParts[0]))
			stats.NetworkTXBytes = parseSizeToBytes(strings.TrimSpace(netParts[1]))
		}
		return stats, nil
	}
	return nil, fmt.Errorf("no stats line found")
}

func getContainerDiskUsage(containerID string) (int64, error) {
	out, err := runCmd("exec", containerID, "du", "-s", "-m", "/home/user")
	if err != nil {
		return 0, err
	}
	parts := strings.Fields(out)
	if len(parts) > 0 {
		if sz, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
			return sz, nil
		}
	}
	return 0, nil
}

func parseSizeToMB(s string) int {
	s = strings.ReplaceAll(s, " ", "")
	if strings.HasSuffix(s, "GiB") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "GiB"), 64)
		return int(v * 1024)
	}
	if strings.HasSuffix(s, "MiB") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "MiB"), 64)
		return int(v)
	}
	if strings.HasSuffix(s, "KiB") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "KiB"), 64)
		return int(v / 1024)
	}
	if strings.HasSuffix(s, "B") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "B"), 64)
		return int(v / 1024 / 1024)
	}
	return 0
}

func parseSizeToBytes(s string) int64 {
	s = strings.ReplaceAll(s, " ", "")
	if strings.HasSuffix(s, "TB") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "TB"), 64)
		return int64(v * 1000 * 1000 * 1000 * 1000)
	}
	if strings.HasSuffix(s, "GB") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "GB"), 64)
		return int64(v * 1000 * 1000 * 1000)
	}
	if strings.HasSuffix(s, "MB") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "MB"), 64)
		return int64(v * 1000 * 1000)
	}
	if strings.HasSuffix(s, "kB") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "kB"), 64)
		return int64(v * 1000)
	}
	if strings.HasSuffix(s, "GiB") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "GiB"), 64)
		return int64(v * 1024 * 1024 * 1024)
	}
	if strings.HasSuffix(s, "MiB") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "MiB"), 64)
		return int64(v * 1024 * 1024)
	}
	if strings.HasSuffix(s, "KiB") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "KiB"), 64)
		return int64(v * 1024)
	}
	if strings.HasSuffix(s, "B") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "B"), 64)
		return int64(v)
	}
	return 0
}

func ListContainers() ([]ContainerInfo, error) {
	out, err := runCmd("ps", "-a", "--filter", "label=owp.account_id", "--format", "{{.ID}}")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	ids := strings.Split(out, "\n")
	var containers []ContainerInfo
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		info, err := GetContainerInfo(id)
		if err != nil {
			continue
		}
		containers = append(containers, *info)
	}
	return containers, nil
}

func StartContainer(nameOrID string) error {
	_, err := runCmd("start", nameOrID)
	return err
}

func StopContainer(nameOrID string) error {
	_, err := runCmd("stop", nameOrID)
	return err
}

func RestartContainer(nameOrID string) error {
	_, err := runCmd("restart", nameOrID)
	return err
}

func RemoveContainer(nameOrID string) error {
	_, err := runCmd("rm", "-f", nameOrID)
	return err
}

func UpdateResourceLimits(nameOrID string, ramLimitMB int, cpuLimit float64) error {
	args := []string{"update"}
	if ramLimitMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", ramLimitMB))
		args = append(args, "--memory-swap", fmt.Sprintf("%dm", ramLimitMB))
	} else {
		args = append(args, "--memory", "0")
		args = append(args, "--memory-swap", "-1")
	}
	if cpuLimit > 0 {
		args = append(args, "--cpus", strconv.FormatFloat(cpuLimit, 'f', 2, 64))
	} else {
		args = append(args, "--cpus", "0")
	}
	args = append(args, nameOrID)
	_, err := runCmd(args...)
	return err
}

func EnsureNetwork() error {
	out, err := runCmd("network", "ls", "--filter", "name=owp-network", "--format", "{{.Name}}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "owp-network" {
		_, err = runCmd("network", "create", "owp-network")
		return err
	}
	return nil
}

func IsContainerRunning(nameOrID string) bool {
	info, err := GetContainerInfo(nameOrID)
	return err == nil && info.Status == "running"
}

func IsContainerCreated(nameOrID string) bool {
	info, err := GetContainerInfo(nameOrID)
	return err == nil && info.ID != ""
}

func ContainerExists(nameOrID string) bool {
	_, err := GetContainerInfo(nameOrID)
	return err == nil
}

func getContainerIP(nameOrID string) (string, error) {
	out, err := runCmd("inspect", "--format", "{{(index .NetworkSettings.Networks \"owp-network\").IPAddress}}", nameOrID)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func GetContainerIP(containerName string) (string, error) {
	return getContainerIP(containerName)
}
