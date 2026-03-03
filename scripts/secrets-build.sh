#!/bin/bash
set -euo pipefail
umask 077

# ============================================================================
# secrets-build.sh - Decrypt and prepare secrets for compose stacks
# ============================================================================
#
# Usage: secrets-build.sh <repo_root> <stack_dir>
#
# Searches for secrets in order of priority:
#   1. {stack_dir}/secrets/*.env, *.secret.env (stack-specific)
#   2. {repo_root}/secrets/*.env, *.secret.env (global)
#
# Environment:
#   SOPS_AGE_KEY_FILE      - Path to Age private key for SOPS decryption
#   SOPS_MISSING_POLICY    - "fail" or "placeholder" (default: fail)
#   MD_SECRETS_CLEANUP     - Remove stale secrets from .runtime (default: true)
#   MD_SECRETS_CACHE       - Skip unchanged files (default: true)
#   LOG_LEVEL              - debug|info|warn|error (default: info)
#   NO_COLOR               - Disable colored output
#
# ============================================================================

# ---- Config ----
: "${SOPS_MISSING_POLICY:=fail}"
: "${MD_SECRETS_CLEANUP:=true}"
: "${MD_SECRETS_CACHE:=true}"
: "${LOG_LEVEL:=info}"
: "${NO_COLOR:=}"

# ---- Args ----
REPO_ROOT="${1:?repo root required}"
STACK_DIR="${2:?stack dir required}"

# ---- Paths ----
GLOBAL_SECRETS_DIR="$(realpath -m "$REPO_ROOT/secrets" 2>/dev/null || echo "$REPO_ROOT/secrets")"
STACK_SECRETS_DIR="$(realpath -m "$STACK_DIR/secrets" 2>/dev/null || echo "$STACK_DIR/secrets")"
RUNTIME_DIR="$GLOBAL_SECRETS_DIR/.runtime"
CACHE_DIR="$GLOBAL_SECRETS_DIR/.cache"
STACK_NAME="$(basename "$STACK_DIR")"

# ---- Colors ----
if [[ -t 1 && -z "$NO_COLOR" ]]; then
  RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[0;33m'
  BLUE='\033[0;34m'; GRAY='\033[0;90m'; NC='\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; BLUE=''; GRAY=''; NC=''
fi

# ---- Logging ----
_lvl() {
  case "$1$LOG_LEVEL" in
    debugdebug|debuginfo|debugwarn|debugerror) return 0;;
    infoinfo|infowarn|infoerror) return 0;;
    warnwarn|warnerror) return 0;;
    errorerror) return 0;;
    *) return 1;;
  esac
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
  _lvl "$level" && printf "${color}[secrets][%s][%s]${NC} %s\n" "$(date +%H:%M:%S)" "$level" "$*"
}

die() { log error "$@"; exit 1; }

# ---- Helpers ----

# Check if file is SOPS encrypted
is_sops_file() {
  local file="$1"
  # Check for SOPS markers
  head -20 "$file" 2>/dev/null | grep -qE '(^sops:|^sops_|ENC\[AES256_GCM|age1[a-z0-9]{58})'
}

# Atomic write to prevent partial files
write_atomic() {
  local src="$1" dst="$2"
  local tmp
  tmp="$(mktemp "${dst}.XXXXXX")"
  chmod 600 "$tmp"
  cat "$src" > "$tmp"
  mv -f "$tmp" "$dst"
}

# Get file checksum for caching
file_hash() {
  local file="$1"
  if command -v sha256sum &>/dev/null; then
    sha256sum "$file" 2>/dev/null | cut -d' ' -f1
  elif command -v shasum &>/dev/null; then
    shasum -a 256 "$file" 2>/dev/null | cut -d' ' -f1
  else
    # Fallback: use modification time
    stat -c %Y "$file" 2>/dev/null || stat -f %m "$file" 2>/dev/null
  fi
}

# Check if file needs processing (cache check)
needs_processing() {
  local src="$1" dst="$2"
  
  [[ "$MD_SECRETS_CACHE" != "true" ]] && return 0
  [[ ! -f "$dst" ]] && return 0
  
  local cache_file="$CACHE_DIR/$(basename "$src").hash"
  [[ ! -f "$cache_file" ]] && return 0
  
  local current_hash cached_hash
  current_hash="$(file_hash "$src")"
  cached_hash="$(cat "$cache_file" 2>/dev/null)"
  
  [[ "$current_hash" != "$cached_hash" ]]
}

# Update cache after processing
update_cache() {
  local src="$1"
  local cache_file="$CACHE_DIR/$(basename "$src").hash"
  mkdir -p "$CACHE_DIR"
  file_hash "$src" > "$cache_file"
}

# Validate .env file format
validate_env_format() {
  local file="$1"
  local line_num=0
  local errors=0
  
  while IFS= read -r line || [[ -n "$line" ]]; do
    line_num=$((line_num + 1))
    
    # Skip empty lines and comments
    [[ -z "$line" || "$line" =~ ^[[:space:]]*# ]] && continue
    
    # Check for valid KEY=VALUE format (KEY can have underscores, numbers)
    if ! [[ "$line" =~ ^[A-Za-z_][A-Za-z0-9_]*= ]]; then
      log warn "invalid format at line $line_num: $line"
      errors=$((errors + 1))
    fi
  done < "$file"
  
  [[ $errors -eq 0 ]]
}

# Process a single secrets file
process_secret_file() {
  local src="$1"
  local dst="$2"
  local base
  base="$(basename "$src")"
  
  # Empty file
  if [[ ! -s "$src" ]]; then
    install -m600 /dev/null "$dst"
    log debug "empty -> ${dst##*/}"
    return 0
  fi
  
  # Check cache
  if ! needs_processing "$src" "$dst"; then
    log debug "cached (unchanged) -> ${dst##*/}"
    return 0
  fi
  
  # SOPS encrypted file
  if is_sops_file "$src"; then
    if [[ -n "${SOPS_AGE_KEY_FILE:-}" && -f "$SOPS_AGE_KEY_FILE" ]]; then
      log info "decrypt ${base} -> ${dst##*/}"
      
      local tmp
      tmp="$(mktemp)"
      trap 'rm -f "$tmp"' RETURN
      
      if ! sops -d "$src" > "$tmp" 2>/dev/null; then
        die "SOPS decryption failed for $base"
      fi
      
      # Validate decrypted content
      if ! validate_env_format "$tmp"; then
        log warn "decrypted file has format warnings: $base"
      fi
      
      write_atomic "$tmp" "$dst"
      update_cache "$src"
      rm -f "$tmp"
      trap - RETURN
    else
      case "$SOPS_MISSING_POLICY" in
        fail)
          die "SOPS_AGE_KEY_FILE not set or missing, cannot decrypt $base"
          ;;
        placeholder)
          install -m600 /dev/null "$dst"
          log warn "no SOPS key; placeholder -> ${dst##*/}"
          ;;
        *)
          die "invalid SOPS_MISSING_POLICY='$SOPS_MISSING_POLICY'"
          ;;
      esac
    fi
  else
    # Plain text file
    log info "copy ${base} -> ${dst##*/}"
    
    # Validate format
    if ! validate_env_format "$src"; then
      log warn "file has format warnings: $base"
    fi
    
    write_atomic "$src" "$dst"
    chmod 600 "$dst"
    update_cache "$src"
  fi
}

# Cleanup stale files from runtime directory
cleanup_stale_secrets() {
  [[ "$MD_SECRETS_CLEANUP" != "true" ]] && return 0
  [[ ! -d "$RUNTIME_DIR" ]] && return 0
  
  log debug "cleaning up stale secrets"
  
  # Build list of expected output files
  local expected=()
  shopt -s nullglob
  for src in "$GLOBAL_SECRETS_DIR"/*.env "$GLOBAL_SECRETS_DIR"/*.secret.env \
             "$STACK_SECRETS_DIR"/*.env "$STACK_SECRETS_DIR"/*.secret.env; do
    [[ -f "$src" ]] || continue
    local base
    base="$(basename "$src")"
    base="${base%.secret.env}"
    [[ "$base" != *.env ]] && base="${base}.env"
    expected+=("$base")
  done
  shopt -u nullglob
  
  # Remove files not in expected list
  for f in "$RUNTIME_DIR"/*.env; do
    [[ -f "$f" ]] || continue
    local base
    base="$(basename "$f")"
    
    local found=false
    for exp in "${expected[@]}"; do
      [[ "$base" == "$exp" ]] && { found=true; break; }
    done
    
    if [[ "$found" == "false" ]]; then
      log info "removing stale secret: $base"
      rm -f "$f"
    fi
  done
}

# ---- Main ----
main() {
  log info "building secrets for stack: $STACK_NAME"
  log debug "global_secrets=$GLOBAL_SECRETS_DIR stack_secrets=$STACK_SECRETS_DIR"
  
  mkdir -p "$RUNTIME_DIR"
  chmod 700 "$RUNTIME_DIR"
  
  local found=0
  local processed=()
  
  shopt -s nullglob
  
  # Process global secrets first
  if [[ -d "$GLOBAL_SECRETS_DIR" ]]; then
    for src in "$GLOBAL_SECRETS_DIR"/*.env "$GLOBAL_SECRETS_DIR"/*.secret.env; do
      [[ -f "$src" ]] || continue
      [[ "$src" == */.runtime/* ]] && continue
      [[ "$src" == */.cache/* ]] && continue
      
      found=1
      local base
      base="$(basename "$src")"
      base="${base%.secret.env}"
      [[ "$base" != *.env ]] && base="${base}.env"
      
      local dst="$RUNTIME_DIR/$base"
      process_secret_file "$src" "$dst"
      processed+=("$base")
    done
  fi
  
  # Process stack-specific secrets (override global)
  if [[ -d "$STACK_SECRETS_DIR" && "$STACK_SECRETS_DIR" != "$GLOBAL_SECRETS_DIR" ]]; then
    for src in "$STACK_SECRETS_DIR"/*.env "$STACK_SECRETS_DIR"/*.secret.env; do
      [[ -f "$src" ]] || continue
      
      found=1
      local base
      base="$(basename "$src")"
      base="${base%.secret.env}"
      [[ "$base" != *.env ]] && base="${base}.env"
      
      local dst="$RUNTIME_DIR/$base"
      
      # Check if already processed from global
      local already_processed=false
      for p in "${processed[@]:-}"; do
        [[ "$p" == "$base" ]] && { already_processed=true; break; }
      done
      
      if [[ "$already_processed" == "true" ]]; then
        log info "stack override: $base"
      fi
      
      process_secret_file "$src" "$dst"
    done
  fi
  
  shopt -u nullglob
  
  # Cleanup stale secrets
  cleanup_stale_secrets
  
  if [[ $found -eq 0 ]]; then
    log warn "no secret files found"
  else
    log info "${GREEN}secrets ready in $RUNTIME_DIR${NC}"
  fi
}

main "$@"
