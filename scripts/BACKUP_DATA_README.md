# Gurud Data Backup Script Usage Guide

## Overview

`backup_data.sh` is an automated script that safely backs up gurud node data and uploads it to AWS S3. This script ensures data integrity by safely stopping the node during backup and restarting it after completion.

## Key Features

- ✅ **Safe Node Stop/Restart**: Safely controls gurud node before and after backup
- ✅ **Data Compression**: tar.gz compression for efficient storage
- ✅ **S3 Upload**: Automatic backup file upload to AWS S3
- ✅ **Local Cleanup**: Automatic deletion of old local backup files based on retention period (configurable)
- ✅ **S3 Cleanup**: Automatic deletion of S3 backup files older than 30 days (fixed policy)
- ✅ **Detailed Logging**: Comprehensive logging of all operations
- ✅ **Error Handling**: Safe node restart even when backup fails

## Prerequisites

### 1. System Requirements
- Linux/Unix environment
- Bash shell
- Sufficient disk space (recommended 2x the size of data folder)

### 2. Required Software
- **AWS CLI**: Required for S3 upload
  ```bash
  # Install AWS CLI (Ubuntu/Debian)
  sudo apt-get update
  sudo apt-get install awscli
  
  # Install AWS CLI (CentOS/RHEL)
  sudo yum install awscli
  
  # Or install via pip
  pip install awscli
  ```

### 3. Directory and File Structure
The following files must exist:
- `/guru/stop.sh` - Script to stop gurud node
- `/guru/start.sh` - Script to start gurud node
- `/guru/home/data/` - Data directory to backup

## Configuration

### 1. AWS Authentication Setup

Configure AWS authentication using one of the following methods:

#### Method A: AWS Profile Usage (Recommended)
```bash
aws configure --profile gurud-backup
# Enter AWS Access Key ID, Secret Access Key, Region
```

#### Method B: Environment Variables
```bash
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"
export AWS_DEFAULT_REGION="ap-northeast-2"  # Seoul region
```

#### Method C: IAM Role Usage (When running on EC2)
Attach an IAM Role with appropriate S3 permissions to your EC2 instance

### 2. S3 Bucket Setup

Create an S3 bucket to store backups and configure appropriate permissions:

```bash
# Create S3 bucket
aws s3 mb s3://your-gurud-backup-bucket

# Example bucket policy (optional)
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::YOUR-ACCOUNT-ID:user/backup-user"
      },
      "Action": [
        "s3:GetObject",
        "s3:PutObject",
        "s3:DeleteObject"
      ],
      "Resource": "arn:aws:s3:::your-gurud-backup-bucket/*"
    }
  ]
}
```

## Usage

### 1. Basic Execution

```bash
# Set environment variables
export S3_BUCKET="your-gurud-backup-bucket"
export AWS_PROFILE="gurud-backup"  # Optional

# Run script with default settings (30 days retention)
cd /path/to/guru-v2
./scripts/backup_data.sh

# Run script with custom local retention period
./scripts/backup_data.sh --retention-days 7
```

### 2. Command Line Options

| Option | Description | Default Value | Required |
|--------|-------------|---------------|----------|
| `--home PATH` | GURU_HOME directory | `/guru` | ❌ |
| `--bucket NAME` | S3 bucket name | `guru-backup-data-bucket` | ❌ |
| `--retention-days N` | Local backup retention period in days | `30` | ❌ |
| `-h, --help` | Show help message | - | ❌ |

### 3. Environment Variable Options

| Environment Variable | Description | Default Value | Required |
|---------------------|-------------|---------------|----------|
| `S3_BUCKET` | S3 bucket name | - | ✅ |
| `S3_PREFIX` | S3 folder path | `gurud-v2.0.1-data-backup` | ❌ |
| `AWS_PROFILE` | AWS profile name | - | ❌ |

### 4. Execution Examples

```bash
# Run with default settings (30 days retention)
export S3_BUCKET="my-gurud-backups"
./scripts/backup_data.sh

# Specify custom local retention period (7 days)
./scripts/backup_data.sh --bucket my-gurud-backups --retention-days 7

# Use command line arguments with custom home directory and local retention
./scripts/backup_data.sh --home /custom/guru/path --bucket my-gurud-backups --retention-days 60

# Specify custom S3 path via environment variable with local retention
export S3_BUCKET="my-gurud-backups"
export S3_PREFIX="production/daily-backups"
./scripts/backup_data.sh --retention-days 14

# Use specific AWS profile with custom local retention
export S3_BUCKET="my-gurud-backups"
export AWS_PROFILE="production"
./scripts/backup_data.sh --retention-days 90
```

## Automation Setup

### Periodic Backups Using Cron

#### 1. Edit Crontab
```bash
crontab -e
```

#### 2. Add Backup Schedule

```bash
# Run backup daily at 2 AM
0 2 * * * cd /path/to/guru-v2 && export S3_BUCKET="your-bucket" && export AWS_PROFILE="default" && ./scripts/backup_data.sh >> /var/log/gurud_backup_cron.log 2>&1

# Run backup every Sunday at 3 AM
0 3 * * 0 cd /path/to/guru-v2 && export S3_BUCKET="your-bucket" && ./scripts/backup_data.sh

# Run backup on 1st of every month at 1 AM
0 1 1 * * cd /path/to/guru-v2 && export S3_BUCKET="your-bucket" && ./scripts/backup_data.sh
```

#### 3. Create Cron Script File (Recommended)

```bash
# Create /etc/cron.d/gurud-backup file
sudo tee /etc/cron.d/gurud-backup << EOF
# Gurud data backup - Daily at 2 AM
0 2 * * * root cd /path/to/guru-v2 && S3_BUCKET="your-bucket" AWS_PROFILE="default" ./scripts/backup_data.sh
EOF
```

## Logging and Monitoring

### 1. Log File Locations
- **Main Log**: `/var/log/gurud_backup.log`
- **Cron Log**: `/var/log/gurud_backup_cron.log` (depending on cron configuration)

### 2. Log Checking Commands

```bash
# Check recent backup logs
tail -f /var/log/gurud_backup.log

# Check backup logs for specific date
grep "2024-01-15" /var/log/gurud_backup.log

# Check error logs only
grep "ERROR" /var/log/gurud_backup.log

# Check backup completion
grep "completed successfully" /var/log/gurud_backup.log
```

### 3. Log Rotation Setup

```bash
# Create /etc/logrotate.d/gurud-backup file
sudo tee /etc/logrotate.d/gurud-backup << EOF
/var/log/gurud_backup.log {
    daily
    missingok
    rotate 30
    compress
    delaycompress
    notifempty
    create 644 root root
}
EOF
```

## Backup File Management

### 1. Backup File Naming Convention
```
gurud_data_backup_YYYYMMDD_HHMMSS.tar.gz
```
Example: `gurud_data_backup_20240115_020000.tar.gz`

### 2. S3 Storage Path
```
s3://your-bucket/gurud-backups/gurud_data_backup_20240115_020000.tar.gz
```

### 3. Local Backup File Cleanup
- Script automatically deletes local backup files older than specified retention period (default: 30 days)
- Retention period can be customized using `--retention-days` option
- Cleanup runs after each successful backup upload

### 4. S3 Backup File Cleanup
- Script automatically deletes S3 backup files older than 30 days (fixed policy)
- Cleanup runs after each successful backup upload
- Only affects files in the configured S3 prefix path

### 5. Backup Restoration Method

```bash
# Download backup file from S3
aws s3 cp s3://your-bucket/gurud-backups/gurud_data_backup_20240115_020000.tar.gz ./

# Stop gurud
/guru/stop.sh

# Backup existing data (for safety)
mv /guru/home/data /guru/home/data.old

# Restore backup file
tar -xzf gurud_data_backup_20240115_020000.tar.gz -C /guru/home/

# Restart gurud
/guru/start.sh
```

## Troubleshooting

### 1. Common Errors and Solutions

#### AWS CLI Errors
```bash
# Error: AWS CLI not found
sudo apt-get install awscli

# Error: Invalid credentials
aws configure list
aws sts get-caller-identity
```

#### Permission Errors
```bash
# Check script execution permissions
chmod +x ./scripts/backup_data.sh

# Check log file permissions
sudo touch /var/log/gurud_backup.log
sudo chmod 644 /var/log/gurud_backup.log
```

#### Insufficient Disk Space
```bash
# Check disk space
df -h

# Clean temporary backup directory
rm -rf /tmp/gurud_backups/*

# Clean old log files
sudo logrotate -f /etc/logrotate.d/gurud-backup
```

### 2. Script Testing

```bash
# Check help
./scripts/backup_data.sh --help

# Dry run (test without actual backup)
# You can modify the script to use echo commands instead of actual operations for testing
```

### 3. Backup Verification

```bash
# Check files uploaded to S3
aws s3 ls s3://your-bucket/gurud-backups/

# Check backup file integrity
tar -tzf /tmp/gurud_backups/gurud_data_backup_20240115_020000.tar.gz | head -10
```

## S3 Backup Management

### 1. Automatic Cleanup Policy
The script includes an automatic cleanup feature that:
- **Local Backup Cleanup**: Deletes local backup files older than the specified retention period (default: 30 days)
  - Retention period can be customized using the `--retention-days` command line option
  - Runs after each successful backup upload
- **S3 Backup Cleanup**: Deletes S3 backup files older than 30 days (fixed policy)
  - Only affects files within the configured S3 prefix path
  - Runs after each successful backup upload
- Logs all deletion operations for audit purposes

### 2. Manual S3 Cleanup
To manually clean up old backups:
```bash
# List all backup files with their dates
aws s3 ls s3://your-bucket/gurud-backups/ --recursive

# Manually delete specific backup file
aws s3 rm s3://your-bucket/gurud-backups/gurud_data_backup_20240101_020000.tar

# Delete all backups older than specific date (be careful!)
aws s3 ls s3://your-bucket/gurud-backups/ --recursive | \
  awk '$1 < "2024-01-01" {print $4}' | \
  xargs -I {} aws s3 rm s3://your-bucket/{}
```

### 3. Local Backup Retention Configuration
You can customize the local backup retention period using the command line option:
```bash
# Keep local backups for 60 days
./scripts/backup_data.sh --bucket my-bucket --retention-days 60

# Keep local backups for 7 days only
./scripts/backup_data.sh --bucket my-bucket --retention-days 7

# Keep local backups for 1 year (365 days)
./scripts/backup_data.sh --bucket my-bucket --retention-days 365
```

For cron jobs, you can specify the local retention period:
```bash
# Cron entry with 14-day local retention
0 2 * * * cd /path/to/guru-v2 && ./scripts/backup_data.sh --bucket my-bucket --retention-days 14
```

**Note**: S3 backup retention is fixed at 30 days and cannot be changed via command line options.

## Security Considerations

### 1. AWS Authentication Security
- Grant minimal permissions to IAM users
- Prefer IAM Roles over Access Keys (in EC2 environment)
- Regular Access Key rotation

### 2. Backup File Encryption
Enable server-side encryption on S3 bucket:
```bash
aws s3api put-bucket-encryption \
  --bucket your-gurud-backup-bucket \
  --server-side-encryption-configuration '{
    "Rules": [
      {
        "ApplyServerSideEncryptionByDefault": {
          "SSEAlgorithm": "AES256"
        }
      }
    ]
  }'
```

### 3. Network Security
- Use VPC endpoints to minimize internet traffic
- Block unnecessary ports with firewall rules

## Performance Optimization

### 1. Compression Optimization
For faster compression, modify the script:
```bash
# Use lz4 instead of gzip (faster, lower compression ratio)
tar --lz4 -cf "$BACKUP_PATH" -C "$GURU_HOME" data

# Or adjust compression level
tar -czf "$BACKUP_PATH" --use-compress-program="gzip -1" -C "$GURU_HOME" data
```

### 2. Parallel Upload
Configure multipart upload for large files:
```bash
aws configure set default.s3.multipart_threshold 64MB
aws configure set default.s3.multipart_chunksize 16MB
aws configure set default.s3.max_concurrent_requests 10
```

## Support and Contact

If you encounter issues with the backup script:
1. Check log files (`/var/log/gurud_backup.log`)
2. Check AWS authentication status (`aws sts get-caller-identity`)
3. Check disk space and permissions
4. Contact development team with logs

---

**⚠️ Important Notes**
- The gurud node will be stopped during backup, causing service downtime
- Ensure sufficient disk space before running backup
- Regularly test backup file restoration capabilities
