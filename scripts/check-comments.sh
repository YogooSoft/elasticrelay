#!/bin/bash
# Script to check for Chinese comments in Go source files
# This enforces our international development standards

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}Checking for Chinese characters in Go source files...${NC}"

# Find Go files with Chinese characters
chinese_files=$(find . -name "*.go" -type f -exec grep -l -P "[\u4e00-\u9fff]" {} \; 2>/dev/null || true)

if [ -n "$chinese_files" ]; then
    echo -e "${RED}❌ Found Chinese characters in the following Go files:${NC}"
    echo "$chinese_files"
    echo
    echo -e "${YELLOW}Details:${NC}"
    
    # Show the specific lines with Chinese characters
    find . -name "*.go" -type f -exec grep -n -P "[\u4e00-\u9fff]" {} /dev/null \; 2>/dev/null || true
    
    echo
    echo -e "${RED}Please translate all Chinese comments to English according to .cursorrules${NC}"
    echo "This is required for international development standards."
    echo
    echo "Example fixes:"
    echo -e "  ${RED}// 连接数据库${NC} → ${GREEN}// Connect to database${NC}"
    echo -e "  ${RED}// 处理错误${NC} → ${GREEN}// Handle error${NC}"
    echo -e "  ${RED}// 并行处理${NC} → ${GREEN}// Process in parallel${NC}"
    echo
    exit 1
else
    echo -e "${GREEN}✅ All Go source files use English comments${NC}"
fi

echo -e "${YELLOW}Checking for Chinese variable/function names...${NC}"

# Check for Chinese identifiers (more strict check)
chinese_identifiers=$(find . -name "*.go" -type f -exec grep -n -P "(func|var|type|const)\s+[^=\s]*[\u4e00-\u9fff]" {} /dev/null \; 2>/dev/null || true)

if [ -n "$chinese_identifiers" ]; then
    echo -e "${RED}❌ Found Chinese characters in identifiers:${NC}"
    echo "$chinese_identifiers"
    echo
    echo -e "${RED}Please use English names for all identifiers (functions, variables, types, constants)${NC}"
    exit 1
else
    echo -e "${GREEN}✅ All identifiers use English names${NC}"
fi

echo -e "${GREEN}✅ All internationalization checks passed!${NC}"
