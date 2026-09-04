#!/usr/bin/env bash
# OpenWebPanel worker diagnostics + auto-fix.
# Runs ON the node host (chrooted by the panel's diag Job, or directly root).
# Usage: worker-diagnose.sh [check|fix] [master|worker]
# Output protocol (parsed by parentd, keep stable):
#   CHECK <name> <ok|fail|skip> <detail...>
#   FIX   <name> <fixed|failed|skipped> <detail...>
set -uo pipefail

MODE="${1:-check}"
ROLE="${2:-worker}"

report() { echo "CHECK $1 $2 ${3:-}"; }
fixed()  { echo "FIX $1 $2 ${3:-}"; }
have()   { command -v "$1" >/dev/null 2>&1; }
svc_active() { systemctl is-active --quiet "$1" 2>/dev/null || service "$1" status >/dev/null 2>&1; }

# Chrooted runs (panel Jobs) inherit the pod netns, where the host's
# 127.0.0.x stub resolver is dead. Swap in working resolvers temporarily.
if grep -qE '^nameserver 127\.' /etc/resolv.conf 2>/dev/null; then
  cp /etc/resolv.conf /tmp/owp-diag-resolv.bak 2>/dev/null || true
  if [[ -s /tmp/owp-pod-resolv.conf ]]; then cp /tmp/owp-pod-resolv.conf /etc/resolv.conf
  else printf 'nameserver 1.1.1.1\nnameserver 8.8.8.8\n' > /etc/resolv.conf; fi
  trap 'mv /tmp/owp-diag-resolv.bak /etc/resolv.conf 2>/dev/null || true' EXIT
fi

# Returns 0 when a TCP/UDP port is locally listening, 1 when closed,
# 2 when it cannot be determined.
port_listening() {
  local proto="$1" port="$2" out=""
  if have ss; then
    # NOTE: capture first — piping straight into `grep -q` under
    # `set -o pipefail` makes results flaky (SIGPIPE races).
    if [[ "$proto" == "udp" ]]; then out=$(ss -uln 2>/dev/null)
    else out=$(ss -tln 2>/dev/null); fi
    echo "$out" | grep -Eq "[:.]${port}([[:space:]]|$)"
    return $?
  fi
  [[ "$proto" == "udp" ]] && return 2
  (echo > "/dev/tcp/127.0.0.1/${port}") >/dev/null 2>&1
}

# --- 1. required ports -------------------------------------------------------
# master serves the API; workers serve kubelet + flannel.
if [[ "$ROLE" == "master" ]]; then PORTS="6443/tcp 8472/udp 10250/tcp";
else PORTS="8472/udp 10250/tcp"; fi
for spec in $PORTS; do
  port="${spec%%/*}"; proto="${spec##*/}"
  if port_listening "$proto" "$port"; then
    report "port-$port" ok "listening ($proto)"
    continue
  elif [[ $? -eq 2 ]]; then
    report "port-$port" skip "cannot probe $proto without ss"
    continue
  else
    report "port-$port" fail "not listening ($proto)"
    if [[ "$MODE" == "fix" ]]; then
      if [[ "$port" == "8472" ]]; then ufw allow 8472/udp >/dev/null 2>&1 || true
      else ufw allow "${port}/tcp" >/dev/null 2>&1 || true; fi
      ufw --force enable >/dev/null 2>&1 || true
      # A closed port is usually firewall, but the service itself may be down.
      systemctl restart k3s-agent >/dev/null 2>&1 || systemctl restart k3s >/dev/null 2>&1 || true
      sleep 5
      if port_listening "$proto" "$port"; then fixed "port-$port" fixed "opened + services restarted"
      else fixed "port-$port" failed "still closed after firewall + restart"; fi
    fi
  fi
done

# --- 2. kubelet / k3s service ------------------------------------------------
if [[ "$ROLE" == "master" ]]; then SVC="k3s"; else SVC="k3s-agent"; fi
if svc_active "$SVC"; then
  report "service-$SVC" ok "active"
else
  report "service-$SVC" fail "not active"
  if [[ "$MODE" == "fix" ]]; then
    systemctl restart "$SVC" >/dev/null 2>&1 || service "$SVC" restart >/dev/null 2>&1 || true
    sleep 8
    if svc_active "$SVC"; then fixed "service-$SVC" fixed "restarted"
    else fixed "service-$SVC" failed "restart did not help — see journalctl -u $SVC"; fi
  fi
fi

# --- 3. disk pressure ---------------------------------------------------------
USE_PCT=$(df / 2>/dev/null | tail -1 | grep -oP '\d+(?=%)' | head -1)
if [[ -z "$USE_PCT" ]]; then report "disk" skip "df unreadable";
elif [[ "$USE_PCT" -ge 95 ]]; then report "disk" fail "root at ${USE_PCT}% (critical)";
elif [[ "$USE_PCT" -ge 85 ]]; then report "disk" fail "root at ${USE_PCT}% (add capacity)";
else report "disk" ok "root at ${USE_PCT}%"; fi

# --- 4. memory pressure -------------------------------------------------------
MEM_TOTAL=$(grep '^MemTotal:' /proc/meminfo 2>/dev/null | awk '{print $2}')
MEM_AVAIL=$(grep '^MemAvailable:' /proc/meminfo 2>/dev/null | awk '{print $2}')
if [[ -z "$MEM_TOTAL" || -z "$MEM_AVAIL" || "$MEM_TOTAL" -eq 0 ]]; then
  report "memory" skip "meminfo unreadable"
else
  FREE_PCT=$(( MEM_AVAIL * 100 / MEM_TOTAL ))
  if [[ "$FREE_PCT" -lt 5 ]]; then report "memory" fail "only ${FREE_PCT}% available"
  else report "memory" ok "${FREE_PCT}% available"; fi
fi

# --- 5. clock sync ------------------------------------------------------------
if have timedatectl && timedatectl show 2>/dev/null | grep -q 'NTPSynchronized=yes'; then
  report "clock" ok "NTP synchronized"
elif have chronyc && chronyc tracking >/dev/null 2>&1; then
  OFF=$(chronyc tracking 2>/dev/null | grep -oP 'System time\s*:\s*\K[0-9.]+' | head -1)
  report "clock" ok "chrony active (offset ${OFF:-?}s)"
else
  report "clock" fail "no NTP sync detected (timedatectl/chrony)"
  if [[ "$MODE" == "fix" ]]; then
    systemctl enable --now systemd-timesyncd >/dev/null 2>&1 || true
    timedatectl set-ntp true >/dev/null 2>&1 || true
    sleep 3
    if have timedatectl && timedatectl show 2>/dev/null | grep -q 'NTPSynchronized=yes'; then fixed "clock" fixed "timesyncd enabled"
    else fixed "clock" failed "install chrony or enable NTP manually"; fi
  fi
fi

# --- 6. DNS -------------------------------------------------------------------
if getent hosts kubernetes.default >/dev/null 2>&1; then
  report "dns" ok "cluster DNS works"
elif getent hosts deb.debian.org >/dev/null 2>&1 || getent hosts archive.ubuntu.com >/dev/null 2>&1; then
  report "dns" ok "external resolution works"
else
  report "dns" fail "cannot resolve (cluster + external)"
fi

# --- 7. container runtime ------------------------------------------------------
if [[ -S /run/k3s/containerd/containerd.sock ]] || have crictl || have ctr; then
  report "runtime" ok "containerd present"
else
  report "runtime" fail "no container runtime found"
fi

# --- 8. hosting deps (nginx + php-fpm socket) ----------------------------------
if have nginx; then report "dep-nginx" ok "$(nginx -v 2>&1 | head -1)"
else report "dep-nginx" fail "nginx not installed (run Install dependencies)"; fi
if ls /run/php/php*-fpm.sock >/dev/null 2>&1; then report "dep-php" ok "php-fpm socket present"
else report "dep-php" fail "no php-fpm socket (run Install dependencies)"; fi

echo "DIAG-DONE role=$ROLE"
