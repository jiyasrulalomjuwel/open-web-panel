#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════════
# OpenWebPanel — K3s server installer (Phase 0: enablement)
#
# Installs a single-node K3s server on THIS host (the future cluster master):
#   sudo bash deploy/k3s/install-k3s-server.sh
#
# What it does:
#   1. Preflight: root, RAM >= 2GB, disk >= 5GB free, ports 6443/8472/10250 free
#   2. Installs k3s server (embedded SQLite datastore, Traefik + local-path kept)
#   3. Waits for the node to become Ready
#   4. Prints the worker join command (uses this host's node-token)
#
# Workers join later with:
#   curl -sfL https://get.k3s.io | K3S_URL=https://<master>:6443 \
#     K3S_TOKEN=<node-token> sh -
#
# NOTE: Traefik is disabled on purpose. OpenWebPanel's own nginx owns host
# ports 80/443; the bundled Traefik (via Klipper svclb hostPorts + KUBE-SERVICES
# PREROUTING rules) would otherwise hijack ALL port-80/443 traffic and answer
# "404 page not found" for every hosted site. Set K3S_KEEP_TRAEFIK=1 to keep it
# (only if the panel does NOT publish 80/443 on this host).
#
# Env overrides:
#   K3S_VERSION   pinned k3s channel/version (default: stable)
#   K3S_EXTRA_ARGS extra args appended to the server (e.g. "--node-name master")
#   K3S_KEEP_TRAEFIK=1  keep the bundled Traefik ingress (see NOTE above)
#   K3S_TLS_SANS  extra --tls-san entries, space separated
# ═══════════════════════════════════════════════════════════════════════════════
set -uo pipefail

log() { echo "[K3S] $*"; }
die() { echo "[K3S-FATAL] $*" >&2; exit 1; }

[[ $EUID -eq 0 ]] || die "must run as root (use sudo)"

# ── Preflight ────────────────────────────────────────────────────────────────
MEM_MB=$(grep '^MemTotal:' /proc/meminfo | awk '{print int($2/1024)}')
[[ "$MEM_MB" -ge 1800 ]] || die "need >= 2GB RAM (have ${MEM_MB}MB)"
DISK_MB=$(df -m / | tail -1 | awk '{print $4}')
[[ "$DISK_MB" -ge 5120 ]] || die "need >= 5GB free disk on / (have ${DISK_MB}MB)"

for p in 6443 8472 10250; do
  if ss -tln 2>/dev/null | grep -q ":${p} "; then
    die "port $p already in use — free it before installing k3s server"
  fi
done

command -v curl >/dev/null || die "curl required"
command -v iptables >/dev/null || log "WARN: iptables not found — k3s networking may fail"

# ── Install ──────────────────────────────────────────────────────────────────
if command -v k3s >/dev/null 2>&1; then
  log "k3s already installed: $(k3s --version 2>/dev/null | head -1)"
else
  log "Installing k3s server (${K3S_VERSION:-stable})..."
  FLAGS="server ${K3S_EXTRA_ARGS:-}"
  if [[ "${K3S_KEEP_TRAEFIK:-0}" != "1" ]]; then
    FLAGS="$FLAGS --disable traefik"
  fi
  for san in ${K3S_TLS_SANS:-}; do
    FLAGS="$FLAGS --tls-san $san"
  done
  # shellcheck disable=SC2086
  curl -sfL https://get.k3s.io | INSTALL_K3S_CHANNEL="${K3S_VERSION:-stable}" \
    INSTALL_K3S_EXEC="$FLAGS" sh - \
    || die "k3s install script failed (check network to get.k3s.io)"
fi

# ── Wait for Ready ───────────────────────────────────────────────────────────
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
for i in $(seq 1 60); do
  if k3s kubectl get nodes 2>/dev/null | grep -q ' Ready'; then
    log "node Ready"
    break
  fi
  [[ $i -eq 60 ]] && die "node not Ready after 5 minutes — see: journalctl -u k3s -n 100"
  sleep 5
done

# ── Report ───────────────────────────────────────────────────────────────────
TOKEN=$(cat /var/lib/rancher/k3s/server/node-token 2>/dev/null) || die "node-token missing"
IP=$(hostname -I 2>/dev/null | awk '{print $1}')
{
  echo ""
  echo "════════════════════════════════════════════════════════"
  echo "  K3s server enabled"
  echo "════════════════════════════════════════════════════════"
  k3s kubectl get nodes -o wide 2>/dev/null | head -5
  echo ""
  echo "  kubeconfig : /etc/rancher/k3s/k3s.yaml (root only)"
  echo "  Workers join with:"
  echo "    curl -sfL https://get.k3s.io | K3S_URL=https://${IP}:6443 K3S_TOKEN=${TOKEN} sh -"
  echo ""
  echo "  Open firewall for workers: 6443/tcp (api), 8472/udp (flannel), 10250/tcp (kubelet)"
  echo "════════════════════════════════════════════════════════"
}
