#!/bin/bash

# 生成 bin 目录下可执行文件的 SHA256 校验码

set -e

# 获取脚本所在目录的上级目录（项目根目录）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
BIN_DIR="${PROJECT_ROOT}/bin"
CHECKSUMS_FILE="${BIN_DIR}/checksums.txt"

# 检查 bin 目录是否存在
if [ ! -d "$BIN_DIR" ]; then
    echo "错误: bin 目录不存在: $BIN_DIR"
    exit 1
fi

# 切换到 bin 目录
cd "$BIN_DIR"

# 检查是否有可执行文件
if ! ls elasticrelay-* 1>/dev/null 2>&1; then
    echo "错误: bin 目录下没有找到 elasticrelay-* 文件"
    exit 1
fi

echo "正在生成 SHA256 校验码..."
echo "目录: $BIN_DIR"
echo ""

# 生成校验码
shasum -a 256 elasticrelay-* > checksums.txt

# 显示结果
echo "已生成校验码:"
echo "----------------------------------------"
cat checksums.txt
echo "----------------------------------------"
echo ""
echo "校验码已保存到: $CHECKSUMS_FILE"
