# Gurud Data Backup Script 사용법

## 개요

`backup_data.sh`는 gurud 노드의 데이터를 안전하게 백업하고 AWS S3에 업로드하는 자동화된 스크립트입니다. 이 스크립트는 백업 과정에서 노드를 안전하게 중지하고 백업 완료 후 다시 시작하여 데이터 무결성을 보장합니다.

## 주요 기능

- ✅ **안전한 노드 중지/재시작**: 백업 전후로 gurud 노드를 안전하게 제어
- ✅ **데이터 압축**: 효율적인 저장을 위한 tar.gz 압축
- ✅ **S3 업로드**: AWS S3에 자동 백업 파일 업로드
- ✅ **로컬 정리**: 디스크 공간 절약을 위한 오래된 백업 파일 자동 삭제
- ✅ **상세 로깅**: 모든 작업에 대한 상세한 로그 기록
- ✅ **에러 처리**: 백업 실패 시에도 노드가 안전하게 재시작

## 전제 조건

### 1. 시스템 요구사항
- Linux/Unix 환경
- Bash shell
- 충분한 디스크 공간 (데이터 폴더 크기의 2배 이상 권장)

### 2. 필수 소프트웨어
- **AWS CLI**: S3 업로드를 위해 필요
  ```bash
  # AWS CLI 설치 (Ubuntu/Debian)
  sudo apt-get update
  sudo apt-get install awscli
  
  # AWS CLI 설치 (CentOS/RHEL)
  sudo yum install awscli
  
  # 또는 pip로 설치
  pip install awscli
  ```

### 3. 디렉토리 및 파일 구조
다음 파일들이 존재해야 합니다:
- `/guru/stop.sh` - gurud 노드 중지 스크립트
- `/guru/start.sh` - gurud 노드 시작 스크립트
- `/guru/home/data/` - 백업할 데이터 디렉토리

## 설정

### 1. AWS 인증 설정

다음 중 하나의 방법으로 AWS 인증을 설정하세요:

#### 방법 A: AWS Profile 사용 (권장)
```bash
aws configure --profile gurud-backup
# AWS Access Key ID, Secret Access Key, Region 입력
```

#### 방법 B: 환경 변수 사용
```bash
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"
export AWS_DEFAULT_REGION="ap-northeast-2"  # 서울 리전
```

#### 방법 C: IAM Role 사용 (EC2에서 실행 시)
EC2 인스턴스에 적절한 S3 권한이 있는 IAM Role을 연결

### 2. S3 버킷 준비

백업을 저장할 S3 버킷을 생성하고 적절한 권한을 설정하세요:

```bash
# S3 버킷 생성
aws s3 mb s3://your-gurud-backup-bucket

# 버킷 정책 예시 (선택사항)
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

## 사용법

### 1. 기본 실행

```bash
# 환경 변수 설정
export S3_BUCKET="your-gurud-backup-bucket"
export AWS_PROFILE="gurud-backup"  # 선택사항

# 스크립트 실행
cd /path/to/guru-v2
./scripts/backup_data.sh
```

### 2. 환경 변수 옵션

| 환경 변수 | 설명 | 기본값 | 필수 |
|-----------|------|--------|------|
| `S3_BUCKET` | S3 버킷 이름 | - | ✅ |
| `S3_PREFIX` | S3 내 폴더 경로 | `gurud-backups` | ❌ |
| `AWS_PROFILE` | AWS 프로필 이름 | - | ❌ |

### 3. 실행 예시

```bash
# 기본 설정으로 실행
export S3_BUCKET="my-gurud-backups"
./scripts/backup_data.sh

# 커스텀 S3 경로 지정
export S3_BUCKET="my-gurud-backups"
export S3_PREFIX="production/daily-backups"
./scripts/backup_data.sh

# 특정 AWS 프로필 사용
export S3_BUCKET="my-gurud-backups"
export AWS_PROFILE="production"
./scripts/backup_data.sh
```

## 자동화 설정

### Cron을 사용한 주기적 백업

#### 1. Crontab 편집
```bash
crontab -e
```

#### 2. 백업 스케줄 추가

```bash
# 매일 새벽 2시에 백업 실행
0 2 * * * cd /path/to/guru-v2 && export S3_BUCKET="your-bucket" && export AWS_PROFILE="default" && ./scripts/backup_data.sh >> /var/log/gurud_backup_cron.log 2>&1

# 매주 일요일 새벽 3시에 백업 실행
0 3 * * 0 cd /path/to/guru-v2 && export S3_BUCKET="your-bucket" && ./scripts/backup_data.sh

# 매월 1일 새벽 1시에 백업 실행
0 1 1 * * cd /path/to/guru-v2 && export S3_BUCKET="your-bucket" && ./scripts/backup_data.sh
```

#### 3. Cron 스크립트 파일 생성 (권장)

```bash
# /etc/cron.d/gurud-backup 파일 생성
sudo tee /etc/cron.d/gurud-backup << EOF
# Gurud 데이터 백업 - 매일 새벽 2시
0 2 * * * root cd /path/to/guru-v2 && S3_BUCKET="your-bucket" AWS_PROFILE="default" ./scripts/backup_data.sh
EOF
```

## 로그 및 모니터링

### 1. 로그 파일 위치
- **메인 로그**: `/var/log/gurud_backup.log`
- **Cron 로그**: `/var/log/gurud_backup_cron.log` (cron 설정에 따라)

### 2. 로그 확인 명령어

```bash
# 최근 백업 로그 확인
tail -f /var/log/gurud_backup.log

# 특정 날짜의 백업 로그 확인
grep "2024-01-15" /var/log/gurud_backup.log

# 에러 로그만 확인
grep "ERROR" /var/log/gurud_backup.log

# 백업 완료 확인
grep "completed successfully" /var/log/gurud_backup.log
```

### 3. 로그 로테이션 설정

```bash
# /etc/logrotate.d/gurud-backup 파일 생성
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

## 백업 파일 관리

### 1. 백업 파일 명명 규칙
```
gurud_data_backup_YYYYMMDD_HHMMSS.tar.gz
```
예: `gurud_data_backup_20240115_020000.tar.gz`

### 2. S3 저장 경로
```
s3://your-bucket/gurud-backups/gurud_data_backup_20240115_020000.tar.gz
```

### 3. 로컬 백업 파일 정리
- 스크립트는 자동으로 최근 3개의 백업 파일만 로컬에 보관
- 임시 백업 디렉토리: `/tmp/gurud_backups`

### 4. 백업 복원 방법

```bash
# S3에서 백업 파일 다운로드
aws s3 cp s3://your-bucket/gurud-backups/gurud_data_backup_20240115_020000.tar.gz ./

# gurud 중지
/guru/stop.sh

# 기존 데이터 백업 (안전을 위해)
mv /guru/home/data /guru/home/data.old

# 백업 파일 복원
tar -xzf gurud_data_backup_20240115_020000.tar.gz -C /guru/home/

# gurud 재시작
/guru/start.sh
```

## 문제 해결

### 1. 일반적인 에러 및 해결책

#### AWS CLI 에러
```bash
# 에러: AWS CLI not found
sudo apt-get install awscli

# 에러: Invalid credentials
aws configure list
aws sts get-caller-identity
```

#### 권한 에러
```bash
# 스크립트 실행 권한 확인
chmod +x ./scripts/backup_data.sh

# 로그 파일 권한 확인
sudo touch /var/log/gurud_backup.log
sudo chmod 644 /var/log/gurud_backup.log
```

#### 디스크 공간 부족
```bash
# 디스크 공간 확인
df -h

# 임시 백업 디렉토리 정리
rm -rf /tmp/gurud_backups/*

# 오래된 로그 파일 정리
sudo logrotate -f /etc/logrotate.d/gurud-backup
```

### 2. 스크립트 테스트

```bash
# 도움말 확인
./scripts/backup_data.sh --help

# 드라이런 (실제 백업 없이 테스트)
# 스크립트를 수정하여 실제 작업 대신 echo 명령어로 테스트 가능
```

### 3. 백업 검증

```bash
# S3에 업로드된 파일 확인
aws s3 ls s3://your-bucket/gurud-backups/

# 백업 파일 무결성 확인
tar -tzf /tmp/gurud_backups/gurud_data_backup_20240115_020000.tar.gz | head -10
```

## 보안 고려사항

### 1. AWS 인증 보안
- IAM 사용자에게 최소 권한만 부여
- Access Key 대신 IAM Role 사용 권장 (EC2 환경)
- 정기적인 Access Key 로테이션

### 2. 백업 파일 암호화
S3 버킷에서 서버 측 암호화 활성화:
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

### 3. 네트워크 보안
- VPC 엔드포인트 사용으로 인터넷 트래픽 최소화
- 방화벽 규칙으로 불필요한 포트 차단

## 성능 최적화

### 1. 압축 최적화
더 빠른 압축을 원하는 경우 스크립트 수정:
```bash
# gzip 대신 lz4 사용 (더 빠름, 압축률 낮음)
tar --lz4 -cf "$BACKUP_PATH" -C "$GURU_HOME" data

# 또는 압축 레벨 조정
tar -czf "$BACKUP_PATH" --use-compress-program="gzip -1" -C "$GURU_HOME" data
```

### 2. 병렬 업로드
대용량 파일의 경우 멀티파트 업로드 설정:
```bash
aws configure set default.s3.multipart_threshold 64MB
aws configure set default.s3.multipart_chunksize 16MB
aws configure set default.s3.max_concurrent_requests 10
```

## 지원 및 문의

백업 스크립트 관련 문제가 발생하면:
1. 로그 파일 확인 (`/var/log/gurud_backup.log`)
2. AWS 인증 상태 확인 (`aws sts get-caller-identity`)
3. 디스크 공간 및 권한 확인
4. 개발팀에 로그와 함께 문의

---

**⚠️ 주의사항**
- 백업 과정에서 gurud 노드가 중지되므로 서비스 다운타임이 발생합니다
- 충분한 디스크 공간을 확보한 후 백업을 실행하세요
- 정기적으로 백업 파일의 복원 가능성을 테스트하세요
