#!/bin/bash

# gurud Data Backup Script
# This script backs up the gurud data folder, compresses it, and uploads to S3

set -e  # Exit on any error

# Default Configuration
DEFAULT_GURU_HOME="/guru"
GURU_VERSION="v2.0.1"
DEFAULT_RETENTION_DAYS=30

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --home)
                GURU_HOME="$2"
                shift 2
                ;;
            --bucket)
                S3_BUCKET="$2"
                shift 2
                ;;
            --retention-days)
                RETENTION_DAYS="$2"
                shift 2
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                echo "ERROR: Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done
}

# Set GURU_HOME (use argument if provided, otherwise use default)
GURU_HOME="${GURU_HOME:-$DEFAULT_GURU_HOME}"

# Set RETENTION_DAYS (use argument if provided, otherwise use default)
RETENTION_DAYS="${RETENTION_DAYS:-$DEFAULT_RETENTION_DAYS}"

# Configuration based on GURU_HOME
DATA_DIR="$GURU_HOME/home/data"
BACKUP_DIR="$GURU_HOME/data_backups"
STOP_SCRIPT="$GURU_HOME/stop.sh"
START_SCRIPT="$GURU_HOME/start.sh"

# S3 Configuration (set these environment variables or use command line arguments)
# AWS_PROFILE or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY should be configured
# S3_BUCKET can be set as environment variable or via --bucket argument
DEFAULT_S3_BUCKET="guru-backup-data-bucket"
S3_PREFIX=${S3_PREFIX:-"gurud-${GURU_VERSION}-data-backup"}

# Generate timestamp for backup filename
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
BACKUP_FILENAME="gurud_${GURU_VERSION}_data_backup_${TIMESTAMP}.tar"
BACKUP_PATH="$BACKUP_DIR/$BACKUP_FILENAME"

# Logging function
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a $GURU_HOME/logs/gurud_${GURU_VERSION}_backup.log
}

# Error handling function
error_exit() {
    log "ERROR: $1"
    # Try to restart gurud even if backup failed
    if [ -f "$START_SCRIPT" ]; then
        log "Attempting to restart gurud after error..."
        bash "$START_SCRIPT" || log "Failed to restart gurud"
    fi
    exit 1
}

# Check prerequisites
check_prerequisites() {
    log "Checking prerequisites..."
    
    # Check if AWS CLI is installed
    if ! command -v aws &> /dev/null; then
        error_exit "AWS CLI is not installed. Please install it first."
    fi
    
    # Check if required directories exist
    if [ ! -d "$DATA_DIR" ]; then
        error_exit "Data directory does not exist: $DATA_DIR"
    fi
    
    if [ ! -f "$STOP_SCRIPT" ]; then
        error_exit "Stop script not found: $STOP_SCRIPT"
    fi
    
    if [ ! -f "$START_SCRIPT" ]; then
        error_exit "Start script not found: $START_SCRIPT"
    fi
    
    # Check S3 bucket access
    if ! aws s3 ls "s3://$S3_BUCKET" &> /dev/null; then
        error_exit "Cannot access S3 bucket: $S3_BUCKET. Please check your AWS credentials and bucket name."
    fi
    
    log "Prerequisites check completed successfully"
}

# Create backup directory
create_backup_dir() {
    log "Creating backup directory: $BACKUP_DIR"
    mkdir -p "$BACKUP_DIR" || error_exit "Failed to create backup directory"
}

# Stop gurud node
stop_gurud() {
    log "Stopping gurud node..."
    if bash "$STOP_SCRIPT"; then
        log "gurud node stopped successfully"
        # Wait a bit to ensure complete shutdown
        sleep 5
    else
        error_exit "Failed to stop gurud node"
    fi
}

# Create tar backup (without compression)
create_backup() {
    log "Creating tar backup of data directory..."
    log "Source: $DATA_DIR"
    log "Backup file: $BACKUP_PATH"
    
    # Calculate data directory size
    DATA_SIZE=$(du -sh "$DATA_DIR" | cut -f1)
    log "Data directory size: $DATA_SIZE"
    
    # Create tar backup (without compression)
    if tar -cf "$BACKUP_PATH" -C "$GURU_HOME" home/data; then
        BACKUP_SIZE=$(du -sh "$BACKUP_PATH" | cut -f1)
        log "Backup created successfully. Archive size: $BACKUP_SIZE"
    else
        error_exit "Failed to create backup archive"
    fi
}

# Upload to S3
upload_to_s3() {
    log "Uploading backup to S3..."
    S3_KEY="$S3_PREFIX/$BACKUP_FILENAME"
    S3_URL="s3://$S3_BUCKET/$S3_KEY"
    
    log "Uploading to: $S3_URL"
    
    if aws s3 cp "$BACKUP_PATH" "$S3_URL"; then
        log "Backup uploaded to S3 successfully"
        # Verify upload
        if aws s3 ls "$S3_URL" &> /dev/null; then
            log "Upload verification successful"
        else
            log "WARNING: Upload verification failed"
        fi
    else
        error_exit "Failed to upload backup to S3"
    fi
}

# Start gurud node
start_gurud() {
    log "Starting gurud node..."
    if bash "$START_SCRIPT"; then
        log "gurud node started successfully"
        # Wait a bit and check if it's running
        sleep 10
        if pgrep -f "gurud" > /dev/null; then
            log "gurud process is running"
        else
            log "WARNING: gurud process not found after start"
        fi
    else
        error_exit "Failed to start gurud node"
    fi
}

# Cleanup old local backups (older than specified retention days)
cleanup_local_backups() {
    log "Cleaning up old local backups (older than $RETENTION_DAYS days)..."
    cd "$BACKUP_DIR"
    
    # Calculate cutoff date based on retention days
    CUTOFF_DATE=$(date -d "$RETENTION_DAYS days ago" +%s)
    
    # Find and remove old backup files
    find . -name "gurud_*_data_backup_*.tar" -type f 2>/dev/null | while read -r backup_file; do
        # Get file modification time
        if command -v gstat &> /dev/null; then
            # macOS with GNU stat (via brew install coreutils)
            file_date=$(gstat -c %Y "$backup_file" 2>/dev/null || echo "0")
        else
            # Linux stat
            file_date=$(stat -c %Y "$backup_file" 2>/dev/null || echo "0")
        fi
        
        # Delete if file is older than cutoff date
        if [[ "$file_date" -lt "$CUTOFF_DATE" && "$file_date" -gt "0" ]]; then
            log "Removing old local backup: $backup_file (age: $(( ($(date +%s) - file_date) / 86400 )) days)"
            rm -f "$backup_file"
        fi
    done
    
    log "Local backup cleanup completed"
}

# Cleanup old S3 backups (older than 30 days)
cleanup_s3_backups() {
    log "Cleaning up old S3 backups (older than 30 days)..."
    
    # Calculate cutoff date (30 days ago)
    CUTOFF_DATE=$(date -d '30 days ago' +%s)
    
    # List all objects in the S3 prefix
    aws s3api list-objects-v2 --bucket "$S3_BUCKET" --prefix "$S3_PREFIX/" --query 'Contents[?Size > `0`].[Key,LastModified]' --output text | while IFS=$'\t' read -r key last_modified; do
        # Skip if key is empty
        if [[ -z "$key" ]]; then
            continue
        fi
        
        # Convert S3 timestamp to epoch time
        # S3 timestamp format: 2024-01-15T02:00:00.000Z
        if command -v gdate &> /dev/null; then
            # macOS with GNU date (via brew install coreutils)
            file_date=$(gdate -d "$last_modified" +%s 2>/dev/null || echo "0")
        else
            # Linux date
            file_date=$(date -d "$last_modified" +%s 2>/dev/null || echo "0")
        fi
        
        # Delete if file is older than cutoff date
        if [[ "$file_date" -lt "$CUTOFF_DATE" && "$file_date" -gt "0" ]]; then
            log "Deleting old S3 backup: $key (modified: $last_modified)"
            if aws s3 rm "s3://$S3_BUCKET/$key"; then
                log "Successfully deleted: $key"
            else
                log "WARNING: Failed to delete: $key"
            fi
        fi
    done
    
    log "S3 backup cleanup completed"
}

# Main backup process
main() {
    log "=== Starting gurud data backup process ==="
    log "GURU_HOME: $GURU_HOME"
    log "DATA_DIR: $DATA_DIR"
    log "S3_BUCKET: $S3_BUCKET"
    log "S3_PREFIX: $S3_PREFIX"
    log "RETENTION_DAYS: $RETENTION_DAYS"
    
    # Trap to ensure gurud is restarted even if script fails
    trap 'log "Script interrupted, attempting to restart gurud..."; bash "$START_SCRIPT" 2>/dev/null || true' INT TERM
    
    check_prerequisites
    create_backup_dir
    stop_gurud
    
    # From this point, we must restart gurud even if backup fails
    create_backup
    upload_to_s3
    start_gurud
    cleanup_local_backups
    cleanup_s3_backups
    
    log "=== Backup process completed successfully ==="
    log "Backup file: $BACKUP_FILENAME"
    log "S3 location: s3://$S3_BUCKET/$S3_PREFIX/$BACKUP_FILENAME"
}

# Usage information
usage() {
    echo "Usage: $0 [options]"
    echo ""
    echo "Options:"
    echo "  --home PATH         Set GURU_HOME directory (default: /guru)"
    echo "  --bucket NAME       Set S3 bucket name (default: guru-backup-data-bucket)"
    echo "  --retention-days N  Set local backup retention period in days (default: 30)"
    echo "  -h, --help          Show this help message"
    echo ""
    echo "Environment variables:"
    echo "  S3_BUCKET       S3 bucket name for backups (can be overridden by --bucket)"
    echo "  S3_PREFIX       S3 prefix/folder (default: gurud-v2.0.1-data-backup)"
    echo "  AWS_PROFILE     AWS profile to use (optional)"
    echo ""
    echo "Examples:"
    echo "  # Use default settings"
    echo "  $0"
    echo ""
    echo "  # Use environment variable for S3 bucket"
    echo "  export S3_BUCKET=my-gurud-backups"
    echo "  $0"
    echo ""
    echo "  # Use command line arguments"
    echo "  $0 --home /custom/guru/path --bucket my-gurud-backups"
    echo ""
    echo "  # With custom local retention period (keep local backups for 7 days)"
    echo "  $0 --bucket my-gurud-backups --retention-days 7"
    echo ""
    echo "  # With AWS profile and custom local retention"
    echo "  export AWS_PROFILE=production"
    echo "  $0 --home ~/.gurud --bucket production-gurud-backups --retention-days 60"
}

# Parse command line arguments first
parse_args "$@"

# Set S3_BUCKET (use argument if provided, otherwise use environment variable, otherwise use default)
S3_BUCKET="${S3_BUCKET:-$DEFAULT_S3_BUCKET}"

# Check if S3_BUCKET is set to a valid value
if [[ -z "$S3_BUCKET" || "$S3_BUCKET" == "your-gurud-backup-bucket" ]]; then
    echo "ERROR: S3_BUCKET must be set either via --bucket argument or S3_BUCKET environment variable"
    echo ""
    usage
    exit 1
fi

# Create logs directory if it doesn't exist
mkdir -p "$GURU_HOME/logs"

# Run main function
main
