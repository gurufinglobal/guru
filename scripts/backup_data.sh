#!/bin/bash

# gurud Data Backup Script
# This script backs up the gurud data folder, compresses it, and uploads to S3

set -e  # Exit on any error

# Configuration
GURU_HOME="/guru"
GURU_VERSION="v2.0.1"
DATA_DIR="$GURU_HOME/home/data"
BACKUP_DIR="$GURU_HOME/data_backups"
STOP_SCRIPT="$GURU_HOME/stop.sh"
START_SCRIPT="$GURU_HOME/start.sh"

# S3 Configuration (set these environment variables)
# AWS_PROFILE or AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY should be configured
# S3_BUCKET should be set as environment variable
S3_BUCKET=${S3_BUCKET:-"your-gurud-backup-bucket"}
S3_PREFIX=${S3_PREFIX:-"gurud-${GURU_VERSION}-data-backup"}

# Generate timestamp for backup filename
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
BACKUP_FILENAME="gurud_${GURU_VERSION}_data_backup_${TIMESTAMP}.tar.gz"
BACKUP_PATH="$BACKUP_DIR/$BACKUP_FILENAME"

# Logging function
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" | tee -a /guru/logs/gurud_${GURU_VERSION}_backup.log
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

# Create compressed backup
create_backup() {
    log "Creating compressed backup of data directory..."
    log "Source: $DATA_DIR"
    log "Backup file: $BACKUP_PATH"
    
    # Calculate data directory size
    DATA_SIZE=$(du -sh "$DATA_DIR" | cut -f1)
    log "Data directory size: $DATA_SIZE"
    
    # Create tar.gz backup
    if tar -czf "$BACKUP_PATH" -C "$GURU_HOME" home/data; then
        BACKUP_SIZE=$(du -sh "$BACKUP_PATH" | cut -f1)
        log "Backup created successfully. Compressed size: $BACKUP_SIZE"
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

# Cleanup old local backups (keep last 3)
cleanup_local_backups() {
    log "Cleaning up old local backups..."
    cd "$BACKUP_DIR"
    
    # Keep only the 3 most recent backups
    ls -t gurud_data_backup_*.tar.gz 2>/dev/null | tail -n +4 | while read -r old_backup; do
        log "Removing old backup: $old_backup"
        rm -f "$old_backup"
    done
    
    log "Local backup cleanup completed"
}

# Main backup process
main() {
    log "=== Starting gurud data backup process ==="
    
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
    
    log "=== Backup process completed successfully ==="
    log "Backup file: $BACKUP_FILENAME"
    log "S3 location: s3://$S3_BUCKET/$S3_PREFIX/$BACKUP_FILENAME"
}

# Usage information
usage() {
    echo "Usage: $0 [options]"
    echo "Environment variables:"
    echo "  S3_BUCKET    - S3 bucket name for backups (required)"
    echo "  S3_PREFIX    - S3 prefix/folder (default: gurud-backups)"
    echo "  AWS_PROFILE  - AWS profile to use (optional)"
    echo ""
    echo "Example:"
    echo "  export S3_BUCKET=my-gurud-backups"
    echo "  export AWS_PROFILE=default"
    echo "  $0"
}

# Check if help is requested
if [[ "$1" == "-h" || "$1" == "--help" ]]; then
    usage
    exit 0
fi

# Check if S3_BUCKET is set
if [[ -z "$S3_BUCKET" || "$S3_BUCKET" == "your-gurud-backup-bucket" ]]; then
    echo "ERROR: S3_BUCKET environment variable must be set"
    echo ""
    usage
    exit 1
fi

# Run main function
main "$@"
