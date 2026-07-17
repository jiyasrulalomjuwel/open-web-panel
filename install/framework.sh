#!/usr/bin/env bash
# ═══════════════════════════════════════════════════════════════════════════════
# OpenWebPanel Installer — TUI Framework
# ═══════════════════════════════════════════════════════════════════════════════

# ─── Terminal Detection ────────────────────────────────────────────────────
_is_tty() { [[ -t 1 && -t 0 && -t 2 ]]; }
HAS_TTY=false; _is_tty && HAS_TTY=true

# ─── Terminal Dimensions ───────────────────────────────────────────────────
if $HAS_TTY; then
  TERM_ROWS=$(tput lines 2>/dev/null || echo 40)
  TERM_COLS=$(tput cols 2>/dev/null || echo 80)
else
  TERM_ROWS=40; TERM_COLS=80
fi

# ─── Colors (ANSI 256) ────────────────────────────────────────────────────
if $HAS_TTY; then
  C_RESET='\033[0m'
  C_BOLD='\033[1m'; C_DIM='\033[2m'; C_ITALIC='\033[3m'; C_REVERSE='\033[7m'

  C_BLACK='\033[38;5;0m'; C_RED='\033[38;5;196m'
  C_GREEN='\033[38;5;82m'; C_YELLOW='\033[38;5;226m'
  C_BLUE='\033[38;5;39m'; C_MAGENTA='\033[38;5;201m'
  C_CYAN='\033[38;5;51m'; C_WHITE='\033[38;5;255m'
  C_ORANGE='\033[38;5;214m'
  C_GRAY='\033[38;5;245m'; C_DARK_GRAY='\033[38;5;236m'

  C_BG_RESET='\033[49m'
  C_BG_BLUE='\033[48;5;24m'; C_BG_DARK='\033[48;5;235m'
  C_BG_GREEN='\033[48;5;22m'; C_BG_RED='\033[48;5;52m'

  C_CLEAR='\033[2J\033[H'
  C_SAVE='\033[s'; C_RESTORE='\033[u'
else
  for _v in C_RESET C_BOLD C_DIM C_ITALIC C_REVERSE \
           C_BLACK C_RED C_GREEN C_YELLOW C_BLUE C_MAGENTA C_CYAN C_WHITE \
           C_ORANGE C_GRAY C_DARK_GRAY \
           C_BG_RESET C_BG_BLUE C_BG_DARK C_BG_GREEN C_BG_RED \
           C_CLEAR C_SAVE C_RESTORE; do
    declare -g "$_v="
  done
fi

# ─── Unicode Glyphs ────────────────────────────────────────────────────────
if $HAS_TTY && locale -a 2>/dev/null | grep -qi 'utf'; then
  G_CHECK="✓"; G_CROSS="✗"; G_BULLET="●"; G_CIRCLE="○"
  G_ARROW="→"; G_DOT="·"; G_BLOCK="█"; G_HALF="▓"; G_DARK="▒"; G_LIGHT="░"
  G_BOX_H="─"; G_BOX_V="│"
  G_ELLIPSIS="…"
else
  G_CHECK="+"; G_CROSS="x"; G_BULLET="*"; G_CIRCLE="o"
  G_ARROW=">"; G_DOT="."; G_BLOCK="#"; G_HALF="#"; G_DARK="#"; G_LIGHT="-"
  G_BOX_H="-"; G_BOX_V="|"
  G_ELLIPSIS="..."
fi

# ─── Stage Definition ──────────────────────────────────────────────────────
STAGE_IDS=(
  "requirements" "prepare" "download" "dependencies"
  "configure" "database" "webserver" "security"
  "services" "validate"
)

STAGE_LABELS=(
  "Checking System Requirements"
  "Preparing Installation"
  "Downloading Required Packages"
  "Installing Dependencies"
  "Configuring Services"
  "Setting Up the Database"
  "Building the Web Server"
  "Applying Security Settings"
  "Starting Services"
  "Running Final Validation"
)

TOTAL_STAGES=${#STAGE_IDS[@]}

declare -A STAGE_STATUS
for _sid in "${STAGE_IDS[@]}"; do STAGE_STATUS[$_sid]="pending"; done

CURRENT_STAGE_ID=""
CURRENT_STAGE_NUM=0
STATUS_MESSAGE=""
STATUS_TYPE="info"
ELAPSED_START=""
ELAPSED_END=""
INSTALL_EXIT_CODE=0
LAST_ERROR_OUTPUT=""

# ─── Stage Status Management ───────────────────────────────────────────────
stage_status_icon() {
  local status=$1
  case "$status" in
    done)    echo "${C_GREEN}${G_CHECK}${C_RESET}" ;;
    fail)    echo "${C_RED}${G_CROSS}${C_RESET}" ;;
    running) echo "${C_YELLOW}${G_BULLET}${C_RESET}" ;;
    warn)    echo "${C_ORANGE}${G_CHECK}${C_RESET}" ;;
    skip)    echo "${C_DIM}${G_ARROW}${C_RESET}" ;;
    *)       echo "${C_DIM}${G_CIRCLE}${C_RESET}" ;;
  esac
}

stage_color() {
  local status=$1
  case "$status" in
    done)    echo "${C_GREEN}" ;;
    fail)    echo "${C_RED}" ;;
    running) echo "${C_YELLOW}" ;;
    warn)    echo "${C_ORANGE}" ;;
    skip)    echo "${C_DIM}" ;;
    *)       echo "${C_DIM}" ;;
  esac
}

set_stage_status() { local id="$1" status="$2"; STAGE_STATUS[$id]="$status"; }

get_stage_label() {
  local id="$1"
  for i in "${!STAGE_IDS[@]}"; do
    [[ "${STAGE_IDS[$i]}" == "$id" ]] && echo "${STAGE_LABELS[$i]}" && return
  done
  echo "$id"
}

get_stage_num() {
  local id="$1"
  for i in "${!STAGE_IDS[@]}"; do
    [[ "${STAGE_IDS[$i]}" == "$id" ]] && echo $((i + 1)) && return
  done
  echo 0
}

# ─── Progress Calculation ──────────────────────────────────────────────────
calc_progress() {
  local done_count=0 total=$TOTAL_STAGES
  for _sid in "${STAGE_IDS[@]}"; do
    local s="${STAGE_STATUS[$_sid]}"
    [[ "$s" == "done" || "$s" == "skip" ]] && ((done_count++))
  done
  echo $((done_count * 100 / total))
}

render_progress_bar() {
  local pct=$1 width=${2:-40}
  local filled=$((pct * width / 100))
  local empty=$((width - filled))
  local i
  printf "${C_BOLD}"
  for ((i=0; i<filled; i++)); do
    if [[ $pct -lt 33 ]]; then
      printf "${C_RED}${G_BLOCK}${C_RESET}${C_BOLD}"
    elif [[ $pct -lt 66 ]]; then
      printf "${C_YELLOW}${G_BLOCK}${C_RESET}${C_BOLD}"
    else
      printf "${C_GREEN}${G_BLOCK}${C_RESET}${C_BOLD}"
    fi
  done
  printf "${C_DIM}"
  for ((i=0; i<empty; i++)); do printf "${G_LIGHT}"; done
  printf "${C_RESET}"
}

has_failures() {
  for _sid in "${STAGE_IDS[@]}"; do
    [[ "${STAGE_STATUS[$_sid]}" == "fail" ]] && return 0
  done
  return 1
}

# ─── Timing ────────────────────────────────────────────────────────────────
timer_start() { ELAPSED_START=$(date +%s); }
timer_stop()  { ELAPSED_END=$(date +%s); }

format_elapsed() {
  local diff=$1 m=$((diff / 60)) s=$((diff % 60))
  printf "%dm %02ds" "$m" "$s"
}

get_elapsed() {
  local now end
  now=$(date +%s)
  if [[ -n "$ELAPSED_END" ]]; then end=$ELAPSED_END; else end=$now; fi
  format_elapsed $((end - ELAPSED_START))
}

# ─── Status Message ────────────────────────────────────────────────────────
set_status() { STATUS_MESSAGE="$1"; STATUS_TYPE="${2:-info}"; }

# ─── Logging ───────────────────────────────────────────────────────────────
init_logging() {
  local log_dir
  log_dir=$(dirname "$INSTALL_LOG" 2>/dev/null || echo "/var/log")
  if ! mkdir -p "$log_dir" 2>/dev/null; then
    echo "[FATAL] Cannot create log directory: $log_dir" >&2
    exit 1
  fi
  if ! : > "$INSTALL_LOG" 2>/dev/null; then
    echo "[FATAL] Cannot write to log file: $INSTALL_LOG" >&2
    exit 1
  fi
  log "INFO" "OpenWebPanel Installer v${OWP_VERSION} started"
  log "INFO" "Repository: ${OWP_REPO} (${OWP_BRANCH})"
  log "INFO" "System: $(uname -a)"
  if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    log "INFO" "OS: $NAME $VERSION_ID"
  fi
}

log() {
  local level="$1" msg="$2"
  local ts
  ts=$(date '+%Y-%m-%d %H:%M:%S')
  echo "[$ts] [$level] $msg" >> "$INSTALL_LOG" 2>/dev/null || true
}

log_info()  { log "INFO" "$1"; }
log_warn()  { log "WARN" "$1"; }
log_error() { log "ERROR" "$1"; }
log_ok()    { log "OK" "$1"; }

# ─── State Management (Resume Support) ─────────────────────────────────────
save_state() {
  local stage_id="$1" status="$2"
  # Atomic write: write to temp then rename
  local tmp="${INSTALL_STATE_FILE}.tmp"
  # Remove any existing entry for this stage, append new one
  if [[ -f "$INSTALL_STATE_FILE" ]]; then
    grep -v "^${stage_id}=" "$INSTALL_STATE_FILE" 2>/dev/null > "$tmp" || true
  else
    : > "$tmp"
  fi
  echo "${stage_id}=${status}" >> "$tmp"
  mv "$tmp" "$INSTALL_STATE_FILE" 2>/dev/null || true
}

load_state() {
  [[ ! -f "$INSTALL_STATE_FILE" ]] && return
  local line id status
  while IFS='=' read -r id status; do
    [[ -z "$id" || -z "$status" ]] && continue
    # Only resume from completed states, not failures
    if [[ "$status" == "done" || "$status" == "warn" || "$status" == "skip" ]]; then
      STAGE_STATUS[$id]="$status"
    fi
  done < "$INSTALL_STATE_FILE"
}

clear_state() {
  rm -f "$INSTALL_STATE_FILE" "${INSTALL_STATE_FILE}.tmp" 2>/dev/null || true
}

is_stage_completed() {
  local id="$1"
  local s="${STAGE_STATUS[$id]:-pending}"
  [[ "$s" == "done" || "$s" == "skip" || "$s" == "warn" ]]
}

# ─── Rollback System ───────────────────────────────────────────────────────
ROLLBACK_STACK=()
ROLLBACK_EXECUTED=false

register_rollback() {
  local handler="$1" description="$2"
  ROLLBACK_STACK+=("$handler|$description")
  log_info "Registered rollback: $description"
}

execute_rollback() {
  $ROLLBACK_EXECUTED && return
  ROLLBACK_EXECUTED=true
  echo "" >> "$INSTALL_LOG" 2>/dev/null || true
  log_warn "=== INSTALLATION FAILED — EXECUTING ROLLBACK ==="
  local i
  for ((i=${#ROLLBACK_STACK[@]}-1; i>=0; i--)); do
    local entry="${ROLLBACK_STACK[$i]}"
    local handler="${entry%%|*}"
    local desc="${entry##*|}"
    log_info "Rolling back: $desc"
    if [[ -n "$handler" ]]; then
      eval "$handler" 2>>"$INSTALL_LOG" || true
    fi
  done
  log_warn "=== ROLLBACK COMPLETE ==="
}

# ─── Password Generation ───────────────────────────────────────────────────
gen_password() {
  local len="${1:-20}"
  if command -v openssl &>/dev/null; then
    openssl rand -base64 30 2>/dev/null | tr -dc 'A-Za-z0-9' | head -c "$len"
  elif command -v python3 &>/dev/null; then
    python3 -c "import secrets; print(secrets.token_hex($len), end='')" 2>/dev/null | head -c "$len"
  else
    head -c 100 /dev/urandom | LC_ALL=C tr -dc 'A-Za-z0-9' | head -c "$len"
  fi
}

# ─── Wait for dpkg lock ────────────────────────────────────────────────────
wait_for_dpkg() {
  # Ensure fuser is available
  if ! command -v fuser &>/dev/null; then
    log_info "fuser not available — skipping dpkg lock wait"
    return 0
  fi
  local waited=0 MAX_WAIT=120
  while fuser /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/cache/apt/archives/lock &>/dev/null 2>&1; do
    local pid pname
    pid=$(fuser /var/lib/dpkg/lock-frontend 2>/dev/null | head -1)
    if [[ -n "$pid" && "$pid" =~ ^[0-9]+$ ]]; then
      pname=$(ps -p "$pid" -o comm= 2>/dev/null || echo "unknown")
    else
      pid="?"; pname="unknown"
    fi
    if [[ $waited -ge $MAX_WAIT ]]; then
      echo "[WARN] dpkg lock held for ${MAX_WAIT}s by ${pname} — force-clearing..."
      kill -9 "$pid" 2>/dev/null || true; sleep 2
      killall -9 unattended-upgrade apt-get apt dpkg 2>/dev/null || true
      rm -f /var/lib/dpkg/lock-frontend /var/lib/dpkg/lock /var/cache/apt/archives/lock /var/lib/apt/lists/lock 2>/dev/null || true
      dpkg --configure -a 2>/dev/null || true; return 0
    fi
    if [[ $((waited % 30)) -eq 0 ]]; then
      echo "[WAIT] ${pname} holds dpkg lock — waiting... (${waited}s)"
    fi
    sleep 2; waited=$((waited + 2))
  done
}

# ─── Utility: find a valid shell for system users ──────────────────────────
find_valid_shell() {
  for s in /usr/sbin/nologin /sbin/nologin /bin/false /usr/bin/false; do
    [[ -x "$s" ]] && echo "$s" && return 0
  done
  echo "/bin/false"
}

# ─── Retry wrapper for network operations ──────────────────────────────────
run_retry() {
  local desc="$1" cmd="$2" max_retries="${3:-3}" delay="${4:-5}"
  local attempt=0 rc=0 output
  while [[ $attempt -lt $max_retries ]]; do
    attempt=$((attempt + 1))
    if [[ $attempt -gt 1 ]]; then
      set_status "Retrying ($attempt/$max_retries): $desc" "warn"
      log_warn "Retry $attempt/$max_retries: $desc"
    fi
    output=$(eval "$cmd" 2>&1)
    rc=$?
    echo "$output" >> "$INSTALL_LOG"
    [[ $rc -eq 0 ]] && break
    [[ $attempt -lt $max_retries ]] && sleep "$delay"
  done
  if [[ $rc -ne 0 ]]; then
    log_error "Failed after $max_retries attempts: $desc"
    log_error "Last output: $output"
    return $rc
  fi
  return 0
}

# ─── Command Execution ─────────────────────────────────────────────────────
run_cmd() {
  local desc="$1" cmd="$2" allow_fail="${3:-false}"
  log_info "Running: $desc"
  log_info "Command: $cmd"
  set_status "$desc" "info"
  local output rc
  output=$(eval "$cmd" 2>&1)
  rc=$?
  echo "$output" >> "$INSTALL_LOG"
  if [[ $rc -ne 0 ]]; then
    local output_preview
    output_preview=$(echo "$output" | tail -5 | tr '\n' '; ')
    log_error "Exit code: $rc"
    log_error "Output: $output"
    LAST_ERROR_OUTPUT="$output"
    if [[ "$allow_fail" != "true" ]]; then
      _fatal_error "$desc failed" "$rc"
    else
      log_warn "$desc failed but was allowed"
      set_status "${C_ORANGE}${G_CROSS} $desc (had issues)${C_RESET}" "warn"
      return $rc
    fi
  fi
  log_ok "$desc"
  return 0
}

run_cmd_quiet() {
  local desc="$1" cmd="$2" allow_fail="${3:-false}"
  log_info "Running: $desc"
  log_info "Command: $cmd"
  local output rc
  output=$(eval "$cmd" 2>&1)
  rc=$?
  echo "$output" >> "$INSTALL_LOG"
  if [[ $rc -ne 0 ]]; then
    log_error "Exit code: $rc"
    log_error "Output: $output"
    LAST_ERROR_OUTPUT="$output"
    [[ "$allow_fail" != "true" ]] && _fatal_error "$desc failed" "$rc"
    return $rc
  fi
  return 0
}

cmd_exists() { command -v "$1" &>/dev/null; }

# ─── TUI Screen Rendering ──────────────────────────────────────────────────
TUI_INITIALIZED=false
SCREEN_RENDER_COUNT=0

_init_screen() {
  $HAS_TTY || return
  TUI_INITIALIZED=true
  printf "${C_CLEAR}"
  trap '_handle_sigint' SIGINT SIGTERM
  trap '_handle_resize' SIGWINCH
  _render_screen
}

_handle_sigint() {
  echo ""
  echo ""
  set_stage_status "$CURRENT_STAGE_ID" "fail"
  save_state "$CURRENT_STAGE_ID" "fail"
  echo -e "  ${C_YELLOW}${G_BULLET} Installation cancelled by user${C_RESET}"
  echo ""
  execute_rollback
  exit 1
}

_handle_resize() {
  TERM_ROWS=$(tput lines 2>/dev/null || echo 40)
  TERM_COLS=$(tput cols 2>/dev/null || echo 80)
  _render_screen
}

_render_screen() {
  $HAS_TTY || return
  ((SCREEN_RENDER_COUNT++))

  local cols=$TERM_COLS
  [[ $cols -lt 60 ]] && cols=60
  [[ $cols -gt 120 ]] && cols=120

  local pct elapsed
  pct=$(calc_progress)
  elapsed=$(get_elapsed)

  local buf=""
  buf+="${C_CLEAR}"

  # ── Header ──
  buf+="\n"
  buf+="  ${C_BOLD}${C_BLUE}${G_BLOCK}${G_BLOCK}${G_BLOCK}${C_RESET} ${C_BOLD}${C_WHITE}OpenWebPanel${C_RESET} ${C_DIM}v${OWP_VERSION}${C_RESET} ${C_BOLD}${C_BLUE}${G_BLOCK}${G_BLOCK}${G_BLOCK}${C_RESET}\n"
  buf+="  ${C_DIM}Web Hosting Control Panel — Automated Setup${C_RESET}\n"
  buf+="\n"

  # ── Stage List with numbers ──
  for i in "${!STAGE_IDS[@]}"; do
    local sid="${STAGE_IDS[$i]}"
    local label="${STAGE_LABELS[$i]}"
    local status="${STAGE_STATUS[$sid]}"
    local icon; icon=$(stage_status_icon "$status")
    local color; color=$(stage_color "$status")
    local num=$((i + 1))
    local num_str
    [[ $num -lt 10 ]] && num_str=" ${num}" || num_str="${num}"
    if [[ "$sid" == "$CURRENT_STAGE_ID" && "$status" == "running" ]]; then
      buf+="  ${C_BOLD}${color}${icon}${C_RESET} ${C_BOLD}[${num_str}/10]${C_RESET} ${C_BOLD}${color}${label}${C_RESET}\n"
    else
      buf+="  ${color}${icon}${C_RESET} ${color}[${num_str}/10] ${label}${C_RESET}\n"
    fi
  done

  buf+="\n"

  # ── Progress Bar ──
  local bar; bar=$(render_progress_bar "$pct" 50)
  local fail_indicator=""
  has_failures && fail_indicator=" ${C_RED}${G_CROSS}${C_RESET}"
  buf+="  ${C_DIM}Progress:${C_RESET} ${bar} ${C_BOLD}${pct}%${C_RESET}${fail_indicator}\n"

  buf+="\n"

  # ── Status Message ──
  local msg_color
  case "$STATUS_TYPE" in
    info)    msg_color="${C_CYAN}" ;;
    warn)    msg_color="${C_ORANGE}" ;;
    error)   msg_color="${C_RED}" ;;
    success) msg_color="${C_GREEN}" ;;
    *)       msg_color="${C_DIM}" ;;
  esac
  buf+="  ${msg_color}${STATUS_MESSAGE}${C_RESET}\n"

  buf+="\n"

  # ── Footer ──
  buf+="  ${C_DIM}${G_BOX_H}${G_BOX_H}${G_BOX_H}${G_BOX_H}${G_BOX_H}${G_BOX_H}${G_BOX_H}${G_BOX_H}${G_BOX_H}${C_RESET}"
  buf+=" ${C_DIM}Elapsed:${C_RESET} ${elapsed}"
  buf+=" ${C_DIM}${G_BOX_V}${C_RESET} ${C_DIM}Log:${C_RESET} ${INSTALL_LOG}"
  if [[ -n "$LAST_ERROR_OUTPUT" ]]; then
    local err_preview
    err_preview=$(echo "$LAST_ERROR_OUTPUT" | head -1 | cut -c1-60)
    buf+="\n  ${C_DIM}Latest error:${C_RESET} ${C_RED}${err_preview}${C_RESET}"
  fi
  buf+="\n"

  echo -e "$buf"
}

_render_screen_final() {
  $HAS_TTY || return
  _render_screen
}

# ─── Stage Runner ──────────────────────────────────────────────────────────
run_stage() {
  local stage_id="$1"
  local stage_func="$2"

  load_state
  if is_stage_completed "$stage_id"; then
    log_info "Stage '${stage_id}' already completed — skipping (resume)"
    return 0
  fi

  CURRENT_STAGE_ID="$stage_id"
  CURRENT_STAGE_NUM=$(get_stage_num "$stage_id")
  set_stage_status "$stage_id" "running"
  save_state "$stage_id" "running"
  _render_screen

  log_info "=== STAGE ${CURRENT_STAGE_NUM}/${TOTAL_STAGES}: $(get_stage_label "$stage_id") ==="
  set_status "Running: $(get_stage_label "$stage_id")" "info"
  _render_screen

  LAST_ERROR_OUTPUT=""
  "$stage_func"
  local rc=$?

  if [[ $rc -eq 0 ]]; then
    set_stage_status "$stage_id" "done"
    save_state "$stage_id" "done"
    log_info "=== STAGE ${CURRENT_STAGE_NUM} COMPLETED ==="
    set_status "${G_CHECK} $(get_stage_label "$stage_id") — done" "success"
  else
    set_stage_status "$stage_id" "fail"
    save_state "$stage_id" "fail"
    log_error "=== STAGE ${CURRENT_STAGE_NUM} FAILED ==="
    INSTALL_EXIT_CODE=$rc
    return $rc
  fi

  _render_screen
  return 0
}

# ─── Error Handler ─────────────────────────────────────────────────────────
_fatal_error() {
  local msg="$1" code="${2:-1}"
  log_error "$msg"
  if [[ -n "$LAST_ERROR_OUTPUT" ]]; then
    log_error "Output: $(echo "$LAST_ERROR_OUTPUT" | head -3 | tr '\n' '; ')"
  fi
  spinner_stop
  set_stage_status "${CURRENT_STAGE_ID}" "fail"
  set_status "${C_RED}${G_CROSS} FATAL: ${msg}${C_RESET}" "error"
  execute_rollback
  INSTALL_EXIT_CODE=$code
  save_state "${CURRENT_STAGE_ID}" "fail"
  _render_screen
  echo ""
  echo -e "  ${C_RED}${G_CROSS} Installation Failed${C_RESET}"
  echo -e "  ${C_DIM}Error:${C_RESET} $msg"
  if [[ -n "$LAST_ERROR_OUTPUT" ]]; then
    echo -e "  ${C_DIM}Details:${C_RESET} $(echo "$LAST_ERROR_OUTPUT" | head -3 | tr '\n' '; ')"
  fi
  echo -e "  ${C_DIM}Log:${C_RESET} $INSTALL_LOG"
  echo ""
  exit $code
}

# ─── Spinner ───────────────────────────────────────────────────────────────
SPINNER_CHARS=("${G_DOT}" "${G_DARK}" "${G_HALF}" "${G_BLOCK}" "${G_HALF}" "${G_DARK}")
SPINNER_PID=""

spinner_start() {
  local msg="$1"
  $HAS_TTY || return
  set_status "$msg" "info"
  SPINNER_PID=$(__spinner_worker "$msg" & echo $!)
  disown 2>/dev/null || true
}

__spinner_worker() {
  local msg="$1" i=0
  while true; do
    local ch="${SPINNER_CHARS[$i]}"
    printf "\033[0;0H\033[K${C_DIM}[${C_RESET}${C_CYAN}${ch}${C_RESET}${C_DIM}]${C_RESET} %s" "$msg" >&2
    i=$(( (i + 1) % ${#SPINNER_CHARS[@]} ))
    sleep 0.15
  done
}

spinner_stop() {
  [[ -z "$SPINNER_PID" ]] && return
  kill "$SPINNER_PID" 2>/dev/null || true
  wait "$SPINNER_PID" 2>/dev/null || true
  SPINNER_PID=""
}

# ─── Non-TTY Output ────────────────────────────────────────────────────────
_nontty_header() {
  echo "╔══════════════════════════════════════════════════╗"
  echo "║     OpenWebPanel Installer v${OWP_VERSION}               ║"
  echo "║     Web Hosting Control Panel                    ║"
  echo "╚══════════════════════════════════════════════════╝"
  echo "Log: $INSTALL_LOG"
  echo ""
}

_nontty_stage() {
  local label="$1"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  ${label}"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

_nontty_status() {
  local type="$1" msg="$2"
  case "$type" in
    info)    echo "  [INFO]  $msg" ;;
    warn)    echo "  [WARN]  $msg" ;;
    error)   echo "  [ERROR] $msg" ;;
    success) echo "  [OK]    $msg" ;;
  esac
}

# ─── Initialization ────────────────────────────────────────────────────────
init_installer() {
  init_logging
  if $HAS_TTY; then
    _init_screen
  else
    _nontty_header
  fi
  timer_start
}

# ─── Final Output ──────────────────────────────────────────────────────────
print_final() {
  spinner_stop
  timer_stop

  if $HAS_TTY; then
    set_status "${G_CHECK} ${G_CHECK} ${G_CHECK} Installation Complete ${G_CHECK} ${G_CHECK} ${G_CHECK}" "success"
    set_stage_status "validate" "done"
    save_state "validate" "done"
    _render_screen

    local elapsed; elapsed=$(get_elapsed)
    echo ""
    echo -e "  ${C_GREEN}${G_CHECK}${C_RESET} ${C_BOLD}OpenWebPanel Installation Complete${C_RESET}"
    echo ""
    echo -e "  ${C_DIM}Elapsed:${C_RESET} ${elapsed}"
    echo -e "  ${C_DIM}Log:${C_RESET} ${INSTALL_LOG}"
    echo ""
  else
    echo ""
    echo "══════════════════════════════════════════════════"
    echo "  Installation Complete"
    echo "══════════════════════════════════════════════════"
    echo "  Elapsed: $(get_elapsed)"
    echo "  Log: $INSTALL_LOG"
    echo ""
  fi

  clear_state
}
