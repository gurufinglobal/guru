#!/bin/bash
# Pre-push local check script
# Usage: ./scripts/pre-push-check.sh [--all]

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=========================================="
echo "🔍 Pre-Push Local Check"
echo "=========================================="

# Check if --all flag is passed
RUN_ALL=false
if [[ "$1" == "--all" ]]; then
	RUN_ALL=true
fi

# 1. Build check
echo -e "\n${YELLOW}[1/6] Building...${NC}"
if make build; then
	echo -e "${GREEN}✓ Build passed${NC}"
else
	echo -e "${RED}✗ Build failed${NC}"
	exit 1
fi

# 2. Go lint check
echo -e "\n${YELLOW}[2/6] Running Go linter...${NC}"
if make lint-go; then
	echo -e "${GREEN}✓ Go lint passed${NC}"
else
	echo -e "${RED}✗ Go lint failed${NC}"
	exit 1
fi

# 3. Unit tests with coverage
echo -e "\n${YELLOW}[3/6] Running unit tests...${NC}"
if make test-unit-cover; then
	echo -e "${GREEN}✓ Unit tests passed${NC}"
else
	echo -e "${RED}✗ Unit tests failed${NC}"
	exit 1
fi

# 4. Proto lint (if proto files changed or --all)
PROTO_CHANGED=$(git diff --cached --name-only | grep -E '\.proto$' || true)
if [[ -n "$PROTO_CHANGED" ]] || [[ "$RUN_ALL" == true ]]; then
	echo -e "\n${YELLOW}[4/6] Running proto linter...${NC}"
	if make proto-lint; then
		echo -e "${GREEN}✓ Proto lint passed${NC}"
	else
		echo -e "${RED}✗ Proto lint failed${NC}"
		exit 1
	fi
else
	echo -e "\n${YELLOW}[4/6] Proto lint skipped (no .proto changes)${NC}"
fi

# 5. Solidity lint (if sol files changed or --all)
SOL_CHANGED=$(git diff --cached --name-only | grep -E '\.sol$' || true)
if [[ -n "$SOL_CHANGED" ]] || [[ "$RUN_ALL" == true ]]; then
	echo -e "\n${YELLOW}[5/6] Running Solidity linter...${NC}"
	if make lint-contracts; then
		echo -e "${GREEN}✓ Solidity lint passed${NC}"
	else
		echo -e "${RED}✗ Solidity lint failed${NC}"
		exit 1
	fi
else
	echo -e "\n${YELLOW}[5/6] Solidity lint skipped (no .sol changes)${NC}"
fi

# 6. Shell script format check (if sh files changed or --all)
SH_CHANGED=$(git diff --cached --name-only | grep -E '\.sh$' || true)
if [[ -n "$SH_CHANGED" ]] || [[ "$RUN_ALL" == true ]]; then
	echo -e "\n${YELLOW}[6/6] Checking shell script format...${NC}"
	if command -v shfmt &>/dev/null; then
		if shfmt -d scripts/*.sh; then
			echo -e "${GREEN}✓ Shell format passed${NC}"
		else
			echo -e "${RED}✗ Shell format failed. Run 'shfmt -w scripts/*.sh' to fix${NC}"
			exit 1
		fi
	else
		echo -e "${YELLOW}⚠ shfmt not installed, skipping shell format check${NC}"
	fi
else
	echo -e "\n${YELLOW}[6/6] Shell format skipped (no .sh changes)${NC}"
fi

echo ""
echo "=========================================="
echo -e "${GREEN}🎉 All checks passed! Ready to push.${NC}"
echo "=========================================="
