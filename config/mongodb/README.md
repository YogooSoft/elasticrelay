# MongoDB 副本集配置说明

## 问题说明

MongoDB 在副本集模式下启用认证时，需要一个 keyFile 用于副本集成员之间的内部认证。

错误信息：
```
BadValue: security.keyFile is required when authorization is enabled with replica sets
```

## 解决方案

已经配置了以下内容：

1. **生成 keyFile**: `config/mongodb/keyfile` - 用于副本集成员间的认证
2. **更新 docker-compose.yml**: 添加了 keyFile 挂载和正确的启动命令
3. **权限处理**: 启动脚本会自动将 keyFile 复制到临时位置并设置正确的权限 (400)

## 重新启动 MongoDB

如果 MongoDB 之前已经启动过但没有 keyFile，需要清理数据并重新初始化：

```bash
# 1. 停止所有服务
docker-compose down

# 2. 清理 MongoDB 数据（重要：这会删除所有 MongoDB 数据）
rm -rf ./data/mongodb/*

# 3. 重新启动服务
docker-compose up -d mongodb

# 4. 查看日志确认启动成功
docker-compose logs -f mongodb

# 5. 等待 MongoDB 健康检查通过后，启动副本集初始化
docker-compose up -d mongodb-init

# 6. 查看副本集初始化日志
docker-compose logs mongodb-init
```

## 验证配置

连接到 MongoDB 并验证副本集状态：

```bash
# 使用 root 用户连接
docker exec -it elasticrelay-mongodb mongosh -u root -p rootpassword --authenticationDatabase admin

# 在 mongosh 中执行
rs.status()

# 应该看到副本集状态为 PRIMARY
```

## keyFile 说明

- **位置**: `config/mongodb/keyfile`
- **权限**: 400 (只读，仅属主可访问)
- **生成方式**: `openssl rand -base64 756 > config/mongodb/keyfile`
- **用途**: 副本集成员之间的内部认证

## 注意事项

1. keyFile 必须在所有副本集成员之间共享
2. keyFile 权限必须是 400 或 600
3. keyFile 内容必须在 6 到 1024 个字符之间
4. 不要将 keyFile 提交到版本控制系统（已添加到 .gitignore）
