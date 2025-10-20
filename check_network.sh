#!/bin/bash

# Quick Network Health Check Launcher
# 간단한 네트워크 상태 체크 실행기

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HEALTH_CHECK_SCRIPT="$SCRIPT_DIR/scripts/network_health_check.sh"

# Check if the health check script exists
if [ ! -f "$HEALTH_CHECK_SCRIPT" ]; then
    echo "Error: Health check script not found at $HEALTH_CHECK_SCRIPT"
    exit 1
fi

# Make sure it's executable
chmod +x "$HEALTH_CHECK_SCRIPT"

# Run the health check script with all arguments passed through
exec "$HEALTH_CHECK_SCRIPT" "$@"
