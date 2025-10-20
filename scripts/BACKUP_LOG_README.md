# Backup Log Manager

Log management script with AWS S3 upload functionality

## 🎯 **Key Improvements**

### ✅ **Original Script Issues Resolved**
- **Insufficient error handling** → Added basic error handling and validation
- **Hardcoded paths** → Configurable via CLI options and environment variables
- **No logging** → Added color-coded simple logging
- **"file changed as we read it" error** → Resolved with smart backup methods

### 🚀 **New Features Added**
- **AWS S3 Upload**: Automatic upload of backup files to S3
- **Configurable Options**: Support for CLI options and environment variables
- **Dry-run Mode**: Test before actual execution
- **Simple Reporting**: Backup status summary
- **Smart Backup**: Automatic detection of active/inactive log files with appropriate backup methods
- **Safe Log Rotation**: Secure backup through process detection

## 🛠️ **Usage**

### Basic Usage
```bash
# Basic execution (no S3 upload)
./scripts/backup_log.sh --skip-s3

# With S3 upload
./scripts/backup_log.sh --s3-bucket your-backup-bucket

# Test execution
./scripts/backup_log.sh --dry-run --s3-bucket your-bucket
```

### Option Configuration
```bash
# Custom configuration
./scripts/backup_log.sh \
    --base-dir /var/log/gurud \
    --moniker validator-01 \
    --retention-days 60 \
    --s3-bucket guru-backups \
    --s3-prefix production/logs

# Custom base directory and log file
./scripts/backup_log.sh \
    --base-dir /custom/path \
    --log-file /custom/path/logs/custom.log

# Quiet mode (for cron)
./scripts/backup_log.sh --quiet --s3-bucket your-bucket
```

### Environment Variables
```bash
export LOG_BASE="/var/log/gurud"
export LOG_FILE="/var/log/gurud/node.log"  # or use base directory
export NODE_MONIKER="mainnet-validator"
export S3_BUCKET="guru-production-logs"
export RETENTION_DAYS="45"

./scripts/backup_log.sh
```

## ⚙️ **AWS S3 Setup**

### 1. AWS CLI Installation and Configuration
```bash
# Install AWS CLI
brew install awscli  # macOS
sudo apt-get install awscli  # Ubuntu

# Configure credentials
aws configure
# Or set environment variables
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"
export AWS_DEFAULT_REGION="us-east-1"
```

### 2. S3 Bucket Creation
```bash
# Create bucket
aws s3 mb s3://your-guru-log-backups --region us-east-1

# Test bucket access
aws s3 ls s3://your-guru-log-backups
```

### 3. IAM Permission Setup
```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "s3:PutObject",
                "s3:GetObject",
                "s3:ListBucket"
            ],
            "Resource": [
                "arn:aws:s3:::your-guru-log-backups",
                "arn:aws:s3:::your-guru-log-backups/*"
            ]
        }
    ]
}
```

## 📅 **Automation Setup**

### Cron Configuration
```bash
# Edit crontab
crontab -e

# Run daily at 3 AM
0 3 * * * /guru/scripts/backup_log.sh --quiet --moniker val01 --skip-s3 --s3-bucket your-bucket >> /guru/logs/backup-log.log 2>&1

# Run every 12 hours
0 3,15 * * * /path/to/guru-v2/scripts/backup_log.sh --quiet --s3-bucket your-bucket
```

### Cron Setup with Environment Variables
```bash
# Create environment variable file
cat > ~/.gurud/log-backup.env << EOF
export S3_BUCKET="guru-production-logs"
export S3_PREFIX="mainnet/validator-01"
export NODE_MONIKER="validator-01"
export RETENTION_DAYS="60"
EOF

# Load environment variables in crontab
0 3 * * * source ~/.gurud/log-backup.env && /path/to/scripts/backup_log.sh --quiet
```

## 📊 **Execution Examples**

### Dry-run Test
```
=== Backup Log Manager ===
Mode: DRY RUN
[INFO] 18:29:33 - Validating configuration...
[SUCCESS] 18:29:33 - Validation passed
[INFO] 18:29:33 - Creating backup: /home/user/.gurud/backup/25-09-05_182933_guru-node.log.tar.gz
[INFO] 18:29:33 - [DRY RUN] Would create backup
[INFO] 18:29:33 - [DRY RUN] Would clear log file
[INFO] 18:29:33 - [DRY RUN] Would upload to: s3://my-bucket/guru-logs/25-09-05_182933_guru-node.log.tar.gz
[INFO] 18:29:33 - Cleaning backups older than 30 days...
[INFO] 18:29:33 - No old backups to clean

=== BACKUP REPORT ===
Directory: /home/user/.gurud/backup
Total backups: 5
Directory size: 2.1M
S3 bucket: my-bucket
=====================

=== COMPLETED ===
```

### Actual Execution
```
=== Backup Log Manager ===
[INFO] 18:29:40 - Validating configuration...
[SUCCESS] 18:29:40 - Validation passed
[INFO] 18:29:40 - Creating backup: /home/user/.gurud/backup/25-09-05_182940_guru-node.log.tar.gz
[SUCCESS] 18:29:41 - Backup created: /home/user/.gurud/backup/25-09-05_182940_guru-node.log.tar.gz (1.2M)
[INFO] 18:29:41 - Uploading to S3: s3://my-bucket/guru-logs/25-09-05_182940_guru-node.log.tar.gz
[SUCCESS] 18:29:45 - Uploaded to S3: s3://my-bucket/guru-logs/25-09-05_182940_guru-node.log.tar.gz
[INFO] 18:29:45 - Cleaning backups older than 30 days...
[SUCCESS] 18:29:45 - Cleaned up 2 old backups

=== BACKUP REPORT ===
Directory: /home/user/.gurud/backup
Total backups: 8
Directory size: 15M
S3 bucket: my-bucket
=====================

=== COMPLETED ===
```

## 🔧 **Troubleshooting**

### Common Issues

#### 1. Log File Not Found
```bash
[ERROR] Log file not found: /path/to/log

# Solutions:
# 1. Check log file path
ls -la ~/.gurud/backup/node.log

# 2. Specify correct path
./scripts/backup_log.sh --log-file /correct/path/to/node.log
```

#### 2. AWS Credentials Error
```bash
[ERROR] AWS credentials not configured

# Solutions:
# 1. Configure AWS CLI
aws configure

# 2. Test credentials
aws sts get-caller-identity
```

#### 3. S3 Bucket Access Error
```bash
# Check bucket existence
aws s3 ls s3://your-bucket-name

# Check permissions
aws s3api get-bucket-acl --bucket your-bucket-name
```

#### 4. Compression Failure ("file changed as we read it" error)
```bash
# This error occurs when the log file is actively being written
# The script automatically handles this as follows:

# 1. Active log detection (using lsof)
lsof /path/to/logfile

# 2. Safe backup methods:
#    - Active log: copy → immediate truncate → compress
#    - Inactive log: direct compression (fallback to copy method if failed)

# Manual safe backup test
cp /path/to/logfile /tmp/backup.log
> /path/to/logfile
tar -czf backup.tar.gz /tmp/backup.log
```

## 🔐 **Security Considerations**

### File Permissions
```bash
# Script execution permission
chmod +x scripts/backup_log.sh

# Log file permissions
chmod 644 ~/.gurud/backup/node.log

# Backup directory permissions
chmod 750 ~/.gurud/backup/
```

### AWS Security
```bash
# Enable S3 bucket encryption
aws s3api put-bucket-encryption \
    --bucket your-bucket \
    --server-side-encryption-configuration \
    '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}'

# Block public access
aws s3api put-public-access-block \
    --bucket your-bucket \
    --public-access-block-configuration \
    BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
```

## 📈 **Monitoring**

### Log Monitoring
```bash
# Check backup logs
tail -f /var/log/backup-log-manager.log

# Check for errors
grep -i error /var/log/backup-log-manager.log

# Check S3 upload status
aws s3 ls s3://your-bucket/guru-logs/ --recursive
```

### Simple Health Check Script
```bash
#!/bin/bash
# check-backup-health.sh

LOG_DIR="$HOME/.gurud/backup"
ALERT_HOURS=25  # Alert if no backup within 25 hours

# Check recent backup
LATEST_BACKUP=$(find "$LOG_DIR" -name "*.tar.gz" -type f -mtime -1 | head -1)

if [ -z "$LATEST_BACKUP" ]; then
    echo "⚠️ WARNING: No backup found in last $ALERT_HOURS hours"
    exit 1
else
    echo "✅ OK: Recent backup found: $(basename "$LATEST_BACKUP")"
    exit 0
fi
```

## 🚀 **Migration from Existing Script**

### Step-by-step Migration
```bash
# 1. Backup existing script
cp /path/to/old-script.sh /path/to/old-script.sh.backup

# 2. Test new script
./scripts/backup_log.sh --dry-run --skip-s3

# 3. Run in parallel (for a few days)
# Existing cron: 0 3 * * * /path/to/old-script.sh
# New cron:      5 3 * * * /path/to/backup_log.sh --skip-s3

# 4. Complete transition
# Replace old script with new script in crontab

# 5. Add S3 functionality
# Remove --skip-s3 option and add --s3-bucket
```

---

This script maintains the basic functionality of the original while improving safety and features with a focus on practicality and stability rather than complex features.