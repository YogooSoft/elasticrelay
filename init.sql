-- 初始化 ElasticRelay 数据库
-- 这个文件在 MySQL 容器首次启动时会自动执行

USE elasticrelay;

-- 创建示例表结构（可根据实际需求修改）
CREATE TABLE IF NOT EXISTS data_sources (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    connection_string TEXT,
    type VARCHAR(100),
    status ENUM('active', 'inactive') DEFAULT 'active',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sync_jobs (
    id INT AUTO_INCREMENT PRIMARY KEY,
    source_id INT,
    target_index VARCHAR(255),
    status ENUM('pending', 'running', 'completed', 'failed') DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    FOREIGN KEY (source_id) REFERENCES data_sources(id)
);

-- 插入示例数据
INSERT INTO data_sources (name, connection_string, type) VALUES
('Example MySQL Source', 'mysql://localhost:3306/example_db', 'mysql'),
('Example PostgreSQL Source', 'postgres://localhost:5432/example_db', 'postgresql');

-- 创建用于测试的表
CREATE TABLE IF NOT EXISTS test_table (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255),
    email VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) DEFAULT CHARSET=utf8mb4;

INSERT INTO test_table (name, email) VALUES
('张三', 'zhangsan@example.com'),
('李四', 'lisi@example.com'),
('王五', 'wangwu@example.com');

-- 创建 users 表
CREATE TABLE IF NOT EXISTS users (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255),
    email VARCHAR(255),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) DEFAULT CHARSET=utf8mb4;

INSERT INTO users (name, email) VALUES
('用户1', 'user1@example.com'),
('用户2', 'user2@example.com');

-- 创建 orders 表
CREATE TABLE IF NOT EXISTS orders (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id INT,
    product_name VARCHAR(255),
    amount DECIMAL(10,2),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
) DEFAULT CHARSET=utf8mb4;

INSERT INTO orders (user_id, product_name, amount) VALUES
(1, '笔记本电脑', 5999.00),
(2, '手机', 2999.00);

-- 授权给 elasticrelay_user 进行复制相关操作的权限
GRANT REPLICATION CLIENT, REPLICATION SLAVE ON *.* TO 'elasticrelay_user'@'%';
GRANT SUPER ON *.* TO 'elasticrelay_user'@'%';
FLUSH PRIVILEGES;
