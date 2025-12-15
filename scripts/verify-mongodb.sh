#!/bin/bash
# MongoDB 验证脚本
# 用于验证 MongoDB 副本集是否正确配置和运行

set -e

echo "🔍 MongoDB 验证检查"
echo "===================="
echo ""

# 检查容器状态
echo "1️⃣ 检查容器状态..."
if docker ps --filter name=elasticrelay-mongodb --format "{{.Status}}" | grep -q "healthy"; then
    echo "   ✅ MongoDB 容器运行正常且健康"
else
    echo "   ❌ MongoDB 容器未运行或不健康"
    docker ps -a --filter name=elasticrelay-mongodb --format "table {{.Names}}\t{{.Status}}"
    exit 1
fi
echo ""

# 检查副本集状态
echo "2️⃣ 检查副本集状态..."
RS_STATUS=$(docker exec elasticrelay-mongodb mongosh -u root -p rootpassword --authenticationDatabase admin --quiet --eval 'rs.status().ok' 2>&1)
if [ "$RS_STATUS" = "1" ]; then
    echo "   ✅ 副本集已初始化"
    RS_STATE=$(docker exec elasticrelay-mongodb mongosh -u root -p rootpassword --authenticationDatabase admin --quiet --eval 'rs.status().members[0].stateStr' 2>&1)
    echo "   📊 副本集状态: $RS_STATE"
else
    echo "   ❌ 副本集未初始化"
    exit 1
fi
echo ""

# 检查认证
echo "3️⃣ 检查 root 用户认证..."
if docker exec elasticrelay-mongodb mongosh -u root -p rootpassword --authenticationDatabase admin --quiet --eval 'db.adminCommand({ping: 1}).ok' 2>&1 | grep -q "1"; then
    echo "   ✅ root 用户认证成功"
else
    echo "   ❌ root 用户认证失败"
    exit 1
fi
echo ""

# 检查应用用户
echo "4️⃣ 检查应用用户 (elasticrelay_user)..."
if docker exec elasticrelay-mongodb mongosh -u elasticrelay_user -p elasticrelay_pass --authenticationDatabase admin elasticrelay --quiet --eval 'db.adminCommand({ping: 1}).ok' 2>&1 | grep -q "1"; then
    echo "   ✅ 应用用户认证成功"
else
    echo "   ❌ 应用用户认证失败"
    exit 1
fi
echo ""

# 检查集合
echo "5️⃣ 检查数据库集合..."
COLLECTIONS=$(docker exec elasticrelay-mongodb mongosh -u elasticrelay_user -p elasticrelay_pass --authenticationDatabase admin elasticrelay --quiet --eval 'db.getCollectionNames().join(", ")' 2>&1)
echo "   📦 集合列表: $COLLECTIONS"
if echo "$COLLECTIONS" | grep -q "users"; then
    echo "   ✅ 集合已创建"
else
    echo "   ⚠️  集合未找到"
fi
echo ""

# 检查数据
echo "6️⃣ 检查示例数据..."
USER_COUNT=$(docker exec elasticrelay-mongodb mongosh -u elasticrelay_user -p elasticrelay_pass --authenticationDatabase admin elasticrelay --quiet --eval 'db.users.countDocuments()' 2>&1)
ORDER_COUNT=$(docker exec elasticrelay-mongodb mongosh -u elasticrelay_user -p elasticrelay_pass --authenticationDatabase admin elasticrelay --quiet --eval 'db.orders.countDocuments()' 2>&1)
PRODUCT_COUNT=$(docker exec elasticrelay-mongodb mongosh -u elasticrelay_user -p elasticrelay_pass --authenticationDatabase admin elasticrelay --quiet --eval 'db.products.countDocuments()' 2>&1)
echo "   👥 users: $USER_COUNT 条记录"
echo "   📦 orders: $ORDER_COUNT 条记录"
echo "   🛍️  products: $PRODUCT_COUNT 条记录"
echo ""

# 检查 keyFile
echo "7️⃣ 检查 keyFile 配置..."
if [ -f "./config/mongodb/keyfile" ]; then
    KEYFILE_SIZE=$(wc -c < ./config/mongodb/keyfile)
    KEYFILE_PERMS=$(stat -f "%OLp" ./config/mongodb/keyfile 2>/dev/null || stat -c "%a" ./config/mongodb/keyfile 2>/dev/null)
    echo "   ✅ keyFile 存在"
    echo "   📏 大小: $KEYFILE_SIZE 字节"
    echo "   🔒 权限: $KEYFILE_PERMS"
    if [ "$KEYFILE_PERMS" = "400" ]; then
        echo "   ✅ 权限正确"
    else
        echo "   ⚠️  权限不是 400，建议修改: chmod 400 ./config/mongodb/keyfile"
    fi
else
    echo "   ❌ keyFile 不存在"
    exit 1
fi
echo ""

echo "✅ 所有检查通过！MongoDB 配置正确。"
echo ""
echo "📚 连接信息："
echo "   主机: localhost:27017"
echo "   副本集: rs0"
echo "   数据库: elasticrelay"
echo "   应用用户: elasticrelay_user / elasticrelay_pass"
echo "   管理员: root / rootpassword"
