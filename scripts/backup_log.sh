#!/bin/bash

# Simple Log Manager with S3 Upload
#
# Usage: ./backup_log.sh [options]

# Removed strict mode settings to fix --quiet option issue

# Default configuration
DEFAULT_BASE="/guru/logs"
BASE="${LOG_BASE:-$DEFAULT_BASE}"
LOGPATH="${LOG_FILE:-/guru/logs/node.log}"
MONIKER="${NODE_MONIKER:-guru-node}"
S3_BUCKET="${S3_BUCKET:-}"
S3_PREFIX="${S3_PREFIX:-guru-logs}"
RETENTION_DAYS="${RETENTION_DAYS:-30}"

# Options
DRY_RUN=false
SKIP_S3=false
QUIET=false
DEBUG=false

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Logging functions
log_info() {
    if [ "$QUIET" = false ]; then
        echo -e "${BLUE}[INFO]${NC} $(date '+%H:%M:%S') - $1"
    fi
}

log_success() {
    if [ "$QUIET" = false ]; then
        echo -e "${GREEN}[SUCCESS]${NC} $(date '+%H:%M:%S') - $1"
    fi
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $(date '+%H:%M:%S') - $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $(date '+%H:%M:%S') - $1" >&2
}

log_debug() {
    if [ "$DEBUG" = true ]; then
        echo -e "${BLUE}[DEBUG]${NC} $(date '+%H:%M:%S') - $1" >&2
    fi
}

# Help function
show_help() {
    cat << EOF
Simple Log Manager v1.0

USAGE:
    $0 [OPTIONS]

OPTIONS:
    --log-file PATH     Log file to backup (default: /guru/logs/node.log)
    --base-dir PATH     Base directory (default: /guru/logs)
    --moniker NAME      Node moniker (default: guru-node)
    --s3-bucket BUCKET  S3 bucket for upload
    --s3-prefix PREFIX  S3 prefix (default: guru-logs)
    --retention-days N  Keep backups for N days (default: 30)
    --skip-s3          Skip S3 upload
    --dry-run          Show what would be done
    --quiet            Suppress output
    --debug            Enable debug output
    --help             Show this help

EXAMPLES:
    # Basic usage
    $0

    # With S3 upload
    $0 --s3-bucket my-backups

    # Custom settings
    $0 --base-dir /var/log/gurud --moniker validator-01 --retention-days 60
    
    # Custom log file and base directory
    $0 --base-dir /custom/path --log-file /custom/path/logs/custom.log

ENVIRONMENT VARIABLES:
    LOG_BASE, LOG_FILE, NODE_MONIKER, S3_BUCKET, S3_PREFIX, RETENTION_DAYS

EOF
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --log-file) LOGPATH="$2"; shift 2 ;;
        --base-dir) BASE="$2"; shift 2 ;;
        --moniker) MONIKER="$2"; shift 2 ;;
        --s3-bucket) S3_BUCKET="$2"; shift 2 ;;
        --s3-prefix) S3_PREFIX="$2"; shift 2 ;;
        --retention-days) RETENTION_DAYS="$2"; shift 2 ;;
        --skip-s3) SKIP_S3=true; shift ;;
        --dry-run) DRY_RUN=true; shift ;;
        --quiet) QUIET=true; shift ;;
        --debug) DEBUG=true; shift ;;
        --help) show_help; exit 0 ;;
        *) log_error "Unknown option: $1"; exit 1 ;;
    esac
done

# Adjust LOGPATH if using default and base directory was changed
if [ "$LOGPATH" = "/guru/logs/node.log" ] && [ "$BASE" != "/guru/logs" ]; then
    LOGPATH="$BASE/node.log"
fi

# Validation
validate() {
    log_info "Validating configuration..."
    log_debug "BASE: $BASE"
    log_debug "LOGPATH: $LOGPATH"
    log_debug "MONIKER: $MONIKER"
    log_debug "S3_BUCKET: $S3_BUCKET"
    log_debug "RETENTION_DAYS: $RETENTION_DAYS"
    log_debug "DRY_RUN: $DRY_RUN"
    log_debug "SKIP_S3: $SKIP_S3"
    log_debug "QUIET: $QUIET"
    
    if [ ! -f "$LOGPATH" ]; then
        log_error "Log file not found: $LOGPATH"
        log_error "Please check the path or create the log file first"
        exit 1
    fi
    
    if ! [[ "$RETENTION_DAYS" =~ ^[0-9]+$ ]]; then
        log_error "Invalid retention days: $RETENTION_DAYS"
        exit 1
    fi
    
    # Check S3 setup if not skipping
    if [ "$SKIP_S3" = false ] && [ -n "$S3_BUCKET" ]; then
        if ! command -v aws >/dev/null 2>&1; then
            log_error "AWS CLI not found. Install it or use --skip-s3"
            exit 1
        fi
        
        if ! aws sts get-caller-identity >/dev/null 2>&1; then
            log_error "AWS credentials not configured"
            exit 1
        fi
    fi
    
    log_success "Validation passed"
    return 0
}

# Check if process is using the log file
check_log_process() {
    # Check if any process is writing to the log file
    if command -v lsof >/dev/null 2>&1; then
        local processes=$(lsof "$LOGPATH" 2>/dev/null | grep -v COMMAND | wc -l | tr -d ' ')
        if [ "$processes" -gt 0 ]; then
            log_info "Detected $processes process(es) using log file"
            return 0
        fi
    fi
    return 1
}

# Create backup and compress
backup_log() {
    local date_stamp=$(date +%y-%m-%d_%H%M%S)
    local backup_name="${date_stamp}_${MONIKER}.log"
    local backup_path="$BASE/backup/$backup_name"
    local compressed_path="$backup_path.tar.gz"
    
    log_info "Creating backup: $compressed_path"
    
    # Check if log file is being actively written
    local is_active=false
    if check_log_process; then
        is_active=true
        log_info "Log file is actively being written to"
    else
        log_info "Log file appears to be idle"
    fi
    
    if [ "$DRY_RUN" = false ]; then
        # Create backup directory if needed
        mkdir -p "$BASE/backup"
        
        if [ "$is_active" = true ]; then
            # Method 1: For active logs - Copy, truncate, then compress
            log_info "Using copy-and-truncate method for active log..."
            
            # Copy the file atomically
            cp "$LOGPATH" "$backup_path" || {
                log_error "Failed to copy log file"
                return 1
            }
            
            # Truncate original file immediately after copy
            > "$LOGPATH" || {
                log_error "Failed to truncate log file"
                rm -f "$backup_path"
                return 1
            }
            
            # Now compress the copy (no rush since original is already truncated)
            log_info "Compressing backup file..."
            tar -czf "$compressed_path" -C "$(dirname "$backup_path")" "$(basename "$backup_path")" || {
                log_error "Failed to create compressed backup"
                rm -f "$backup_path"
                return 1
            }
            
        else
            # Method 2: For idle logs - Direct compression
            log_info "Using direct compression method for idle log..."
            
            # Try direct compression with warning suppression
            tar -czf "$compressed_path" -C "$(dirname "$LOGPATH")" "$(basename "$LOGPATH")" 2>/dev/null || {
                # If direct compression fails, fall back to copy method
                log_warning "Direct compression failed, falling back to copy method..."
                
                cp "$LOGPATH" "$backup_path" || {
                    log_error "Failed to copy log file"
                    return 1
                }
                
                tar -czf "$compressed_path" -C "$(dirname "$backup_path")" "$(basename "$backup_path")" || {
                    log_error "Failed to create compressed backup"
                    rm -f "$backup_path"
                    return 1
                }
            }
            
            # Clear the original log file
            > "$LOGPATH" || {
                log_error "Failed to truncate log file"
                return 1
            }
        fi
        
        # Clean up temporary file if it exists
        [ -f "$backup_path" ] && rm -f "$backup_path"
        
        # Brief pause
        sleep 1
        
        if [ -f "$compressed_path" ]; then
            local size=$(ls -lh "$compressed_path" | awk '{print $5}')
            log_success "Backup created: $compressed_path ($size)"
            
            # Upload to S3 if configured
            upload_s3 "$compressed_path"
        else
            log_error "Backup file not created"
            return 1
        fi
    else
        log_info "[DRY RUN] Would create: $compressed_path"
        log_info "[DRY RUN] Would clear: $LOGPATH"
        
        # Simulate S3 upload in dry run
        upload_s3 "$compressed_path"
    fi
    
    return 0
}

# Upload to S3
upload_s3() {
    local file="$1"
    local filename=$(basename "$file")
    local s3_uri="s3://$S3_BUCKET/$S3_PREFIX/$filename"
    
    if [ "$SKIP_S3" = true ] || [ -z "$S3_BUCKET" ]; then
        log_info "Skipping S3 upload"
        return 0
    fi
    
    log_info "Uploading to S3: $s3_uri"
    
    if [ "$DRY_RUN" = false ]; then
        # Check if file already exists
        if aws s3 ls "$s3_uri" >/dev/null 2>&1; then
            log_warning "File already exists in S3: $s3_uri"
            return 0
        fi
        
        # Upload to S3
        if aws s3 cp "$file" "$s3_uri" --storage-class STANDARD_IA >/dev/null 2>&1; then
            log_success "Uploaded to S3: $s3_uri"
        else
            log_error "Failed to upload to S3"
            return 1
        fi
    else
        log_info "[DRY RUN] Would upload to: $s3_uri"
    fi
}

# Clean old backups
cleanup_old() {
    local logs_dir="$BASE/backup"
    local retention_minutes=$((RETENTION_DAYS * 24 * 60))
    
    log_info "Cleaning backups older than $RETENTION_DAYS days..."
    
    if [ ! -d "$logs_dir" ]; then
        log_info "Logs directory not found, skipping cleanup"
        return 0
    fi
    
    local old_files=$(find "$logs_dir" -name "*.tar.gz" -type f -mmin +$retention_minutes 2>/dev/null || true)
    
    if [ -z "$old_files" ]; then
        log_info "No old backups to clean"
        return 0
    fi
    
    local count=0
    while IFS= read -r file; do
        if [ -f "$file" ]; then
            log_info "Deleting old backup: $(basename "$file")"
            if [ "$DRY_RUN" = false ]; then
                if rm -f "$file"; then
                    count=$((count + 1))
                fi
            else
                log_info "[DRY RUN] Would delete: $file"
                count=$((count + 1))
            fi
        fi
    done <<< "$old_files"
    
    [ "$count" -gt 0 ] && log_success "Cleaned up $count old backups"
}

# Generate simple report
report() {
    local logs_dir="$BASE/backup"
    
    if [ ! -d "$logs_dir" ]; then
        return 0
    fi
    
    local total=$(find "$logs_dir" -name "*.tar.gz" -type f 2>/dev/null | wc -l | tr -d ' ')
    local size=$(du -sh "$logs_dir" 2>/dev/null | cut -f1 || echo "unknown")
    
    if [ "$QUIET" = false ]; then
        cat << EOF

=== BACKUP REPORT ===
Directory: $logs_dir
Total backups: $total
Directory size: $size
S3 bucket: ${S3_BUCKET:-"Not configured"}
=====================

EOF
    fi
}

# Main execution
main() {
    if [ "$QUIET" = false ]; then
        echo "=== Simple Log Manager ==="
    fi
    
    if [ "$DRY_RUN" = true ] && [ "$QUIET" = false ]; then
        echo "Mode: DRY RUN"
    fi
    
    # Validate configuration first
    if ! validate; then
        log_error "Validation failed"
        exit 1
    fi
    
    # Create backup
    if ! backup_log; then
        log_error "Backup failed"
        exit 1
    fi
    
    # Cleanup old backups
    cleanup_old
    
    # Generate report
    report
    
    if [ "$QUIET" = false ]; then
        echo "=== COMPLETED ==="
    fi
}

# Run main function
main
