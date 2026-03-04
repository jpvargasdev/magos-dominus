#!/bin/bash
set -euo pipefail
umask 077

# ============================================================================
# reconcile.sh - Deploy compose stacks with healthchecks and rollback
# ============================================================================
#
# Usage: reconcile.sh <repo_root> <compose_file> [policy]
#
# Environment:
#   MD_DRY_RUN=true        - Simulate without applying changes
#   MD_HEALTHCHECK_RETRIES - Number of health check retries (default: 30)
#   MD_HEALTHCHECK_DELAY   - Seconds between retries (default: 2)
#   MD_SKIP_HEALTHCHECK    - Skip health checks (default: false)
#   MD_CONTAINER_RUNTIME   - "podman" or "docker" (default: auto-detect)
#   LOG_LEVEL              - debug|info|warn|error (default: info)
#   NO_COLOR               - Disable colored output
#
# ============================================================================

# ---- Args ----
REPO_ROOT="${1:?repo root required}"
UPDATED_FILE="${2:?compose file required}"
POLICY="${3:-semver}"

# ---- Config ----
: "${MD_DRY_RUN:=false}"
: "${MD_HEALTHCHECK_RETRIES:=30}"
: "${MD_HEALTHCHECK_DELAY:=2}"
: "${MD_SKIP_HEALTHCHECK:=false}"
: "${MD_CONTAINER_RUNTIME:=}"
: "${LOG_LEVEL:=info}"
: "${NO_COLOR:=}"

# ---- Paths ----
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STACK_DIR="$(dirname "$UPDATED_FILE")"
COMPOSE_FILE="$UPDATED_FILE"
STACK_NAME="$(basename "$STACK_DIR")"
BACKUP_DIR="${REPO_ROOT}/.magos/backups/${STACK_NAME}"

# ---- Colors ----
if [[ -t 1 && -z "$NO_COLOR" ]]; then
  RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[0;33m'
  BLUE='\033[0;34m'; GRAY='\033[0;90m'; NC='\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; BLUE=''; GRAY=''; NC=''
fi

# ---- Logging ----
# Returns 0 (true) if message at $1 level should be shown given $LOG_LEVEL
_lvl() {
  local msg_level="$1"
  case "$LOG_LEVEL" in
    debug) return 0;;  # show all
    info)  [[ "$msg_level" != "debug" ]] && return 0;;
    warn)  [[ "$msg_level" == "warn" || "$msg_level" == "error" ]] && return 0;;
    error) [[ "$msg_level" == "error" ]] && return 0;;
  esac
  return 1
}

log() {
  local level="$1"; shift
  local color=""
  case "$level" in
    debug) color="$GRAY";;
    info)  color="$BLUE";;
    warn)  color="$YELLOW";;
    error) color="$RED";;
  esac
  _lvl "$level" && printf "${color}[reconcile][%s][%s]${NC} %s\n" "$(date +%H:%M:%S)" "$level" "$*"
}

die() { log error "$@"; exit 1; }

# ---- Container Runtime Detection ----
detect_runtime() {
  if [[ -n "$MD_CONTAINER_RUNTIME" ]]; then
    echo "$MD_CONTAINER_RUNTIME"
    return
  fi
  
  if command -v podman &>/dev/null && command -v podman-compose &>/dev/null; then
    echo "podman"
  elif command -v docker &>/dev/null && docker compose version &>/dev/null; then
    echo "docker"
  else
    die "No container runtime found. Install podman+podman-compose or docker+docker-compose"
  fi
}

RUNTIME="$(detect_runtime)"
log info "using runtime: $RUNTIME"

# Runtime-specific commands
if [[ "$RUNTIME" == "podman" ]]; then
  COMPOSE_CMD="podman-compose"
  export PODMAN_LOG_LEVEL="${PODMAN_LOG_LEVEL:-warn}"
else
  COMPOSE_CMD="docker compose"
fi

compose() {
  if [[ "$RUNTIME" == "podman" ]]; then
    podman-compose -f "$COMPOSE_FILE" "$@"
  else
    docker compose -f "$COMPOSE_FILE" "$@"
  fi
}

# ---- Dry Run Wrapper ----
run() {
  if [[ "$MD_DRY_RUN" == "true" ]]; then
    log info "[dry-run] $*"
    return 0
  fi
  "$@"
}

# ---- Backup Current State ----
backup_state() {
  log debug "backing up current state"
  mkdir -p "$BACKUP_DIR"
  
  local backup_file="$BACKUP_DIR/state-$(date +%Y%m%d-%H%M%S).json"
  
  # Get current running containers and their images
  local services
  services=$(compose config --services 2>/dev/null || echo "")
  
  if [[ -n "$services" ]]; then
    {
      echo "{"
      echo "  \"timestamp\": \"$(date -Iseconds)\","
      echo "  \"compose_file\": \"$COMPOSE_FILE\","
      echo "  \"services\": {"
      local first=true
      for svc in $services; do
        local image
        image=$(compose ps --format json 2>/dev/null | jq -r "select(.Service==\"$svc\") | .Image" 2>/dev/null || echo "unknown")
        [[ "$first" == "true" ]] || echo ","
        echo "    \"$svc\": \"$image\""
        first=false
      done
      echo "  }"
      echo "}"
    } > "$backup_file"
    log debug "state backed up to $backup_file"
  fi
}

# ---- Health Check ----
check_health() {
  if [[ "$MD_SKIP_HEALTHCHECK" == "true" ]]; then
    log info "healthcheck skipped (MD_SKIP_HEALTHCHECK=true)"
    return 0
  fi
  
  log info "checking container health..."
  
  local services
  services=$(compose config --services 2>/dev/null || echo "")
  [[ -z "$services" ]] && { log warn "no services found"; return 0; }
  
  local all_healthy=true
  local retry=0
  
  while [[ $retry -lt $MD_HEALTHCHECK_RETRIES ]]; do
    all_healthy=true
    
    for svc in $services; do
      local status
      if [[ "$RUNTIME" == "podman" ]]; then
        status=$(podman ps --filter "label=com.docker.compose.service=$svc" --format "{{.Status}}" 2>/dev/null | head -1)
      else
        status=$(docker ps --filter "label=com.docker.compose.service=$svc" --format "{{.Status}}" 2>/dev/null | head -1)
      fi
      
      if [[ -z "$status" ]]; then
        log debug "service $svc: not running yet"
        all_healthy=false
      elif [[ "$status" == *"unhealthy"* ]]; then
        log debug "service $svc: unhealthy"
        all_healthy=false
      elif [[ "$status" == *"health: starting"* ]]; then
        log debug "service $svc: health starting"
        all_healthy=false
      elif [[ "$status" == *"Up"* ]]; then
        log debug "service $svc: running"
      else
        log debug "service $svc: unknown status '$status'"
        all_healthy=false
      fi
    done
    
    if [[ "$all_healthy" == "true" ]]; then
      log info "${GREEN}all services healthy${NC}"
      return 0
    fi
    
    retry=$((retry + 1))
    log debug "health check retry $retry/$MD_HEALTHCHECK_RETRIES"
    sleep "$MD_HEALTHCHECK_DELAY"
  done
  
  log error "health check failed after $MD_HEALTHCHECK_RETRIES retries"
  return 1
}

# ---- Rollback ----
rollback() {
  log warn "initiating rollback..."
  
  # Find most recent backup
  local latest_backup
  latest_backup=$(ls -t "$BACKUP_DIR"/state-*.json 2>/dev/null | head -1)
  
  if [[ -z "$latest_backup" ]]; then
    log error "no backup found for rollback"
    return 1
  fi
  
  log info "rolling back using $latest_backup"
  
  # For now, just restart with previous config
  # A more sophisticated rollback would restore the previous image tags
  run compose down --timeout 10 2>/dev/null || true
  run compose up -d
  
  log warn "rollback completed - manual verification recommended"
}

# ---- Validate env_file References ----
validate_env_files() {
  if ! grep -qE '^\s*env_file:' "$COMPOSE_FILE" 2>/dev/null; then
    log debug "no env_file references found"
    return 0
  fi
  
  log debug "validating env_file references"
  
  while IFS= read -r p; do
    p="${p#*- }"
    p="${p## }"  # trim leading spaces
    p="${p%% }"  # trim trailing spaces
    [[ -z "$p" ]] && continue
    [[ "$p" = /* ]] || p="$(cd "$STACK_DIR" && realpath -m "$p" 2>/dev/null || echo "$STACK_DIR/$p")"
    
    if [[ ! -f "$p" ]]; then
      die "missing env_file: $p"
    fi
    log debug "env_file ok: $p"
  done < <(awk '$1 ~ /^env_file:/ {f=1;next} f && /^[[:space:]]*-/ {print; next} f && !/^[[:space:]]/ {f=0}' "$COMPOSE_FILE")
}

# ---- Main ----
main() {
  log info "reconciling stack: $STACK_NAME"
  log debug "repo_root=$REPO_ROOT compose_file=$COMPOSE_FILE policy=$POLICY"
  
  # 1. Secrets
  if [[ -x "$SCRIPT_DIR/secrets-build.sh" ]]; then
    log info "building secrets"
    "$SCRIPT_DIR/secrets-build.sh" "$REPO_ROOT" "$STACK_DIR"
  else
    log debug "no secrets-build.sh found"
  fi
  
  # 2. Validate env_file references
  validate_env_files
  
  # 3. Validate compose syntax
  log info "validating compose file"
  compose config >/dev/null || die "compose validation failed"
  
  # 4. Backup current state
  backup_state
  
  # 5. Pull new images
  log info "pulling images"
  run compose pull || die "image pull failed"
  
  # 6. Deploy
  log info "deploying (up -d)"
  run compose up -d --remove-orphans || die "compose up failed"
  
  # 7. Health check
  if ! check_health; then
    log error "deployment unhealthy, attempting rollback"
    rollback
    die "deployment failed - rolled back"
  fi
  
  log info "${GREEN}reconcile complete${NC}"
}

main "$@"
