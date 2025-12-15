#!/bin/bash
# MongoDB 重置脚本
# 用于清理旧数据并重新初始化 MongoDB 副本集

set -e

# 获取脚本所在目录的父目录（项目根目录）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR"

echo "⚠️  警告：此操作将删除所有 MongoDB 数据！"
echo "按 Ctrl+C 取消，或按 Enter 继续..."
read

echo "🛑 停止 MongoDB 相关服务..."
docker-compose stop mongodb mongodb-init 2>/dev/null || true

echo "🗑️  删除 MongoDB 容器..."
docker-compose rm -f mongodb mongodb-init 2>/dev/null || true

echo "🧹 清理 MongoDB 数据目录..."
rm -rf ./data/mongodb/*

echo "✅ 清理完成！"
echo ""
echo "📦 启动 MongoDB..."
docker-compose up -d mongodb

echo "⏳ 等待 MongoDB 健康检查通过..."
for i in {1..30}; do
  if docker inspect --format='{{.State.Health.Status}}' elasticrelay-mongodb 2>/dev/null | grep -q "healthy"; then
    echo "✅ MongoDB 已就绪！"
    break
  fi
  echo -n "."
  sleep 2
done
echo ""

echo "🔧 初始化副本集..."
docker-compose up mongodb-init

echo "⏳ 等待副本集初始化完成..."
sleep 5

echo ""
echo "✅ MongoDB 重置完成！"
echo ""
echo "📊 验证副本集状态..."
docker exec elasticrelay-mongodb mongosh -u root -p rootpassword --authenticationDatabase admin --eval 'rs.status()' | grep -E "(stateStr|ok)"

echo ""
echo "📚 常用命令："
echo "  查看日志: docker-compose logs -f mongodb"
echo "  查看状态: docker exec -it elasticrelay-mongodb mongosh -u root -p rootpassword --authenticationDatabase admin --eval 'rs.status()'"
echo "  查看集合: docker exec -it elasticrelay-mongodb mongosh -u elasticrelay_user -p elasticrelay_pass --authenticationDatabase admin elasticrelay --eval 'db.getCollectionNames()'"
