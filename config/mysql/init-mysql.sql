-- MySQL 初始化脚本用于ElasticRelay CDC
-- 创建用户、数据库和必要的权限设置

-- 创建ElasticRelay专用用户（如果不存在）
CREATE USER IF NOT EXISTS 'elasticrelay_user'@'%' IDENTIFIED BY 'elasticrelay_pass';

-- 授予必要的权限
GRANT SELECT, RELOAD, SHOW DATABASES, REPLICATION SLAVE, REPLICATION CLIENT ON *.* TO 'elasticrelay_user'@'%';
GRANT ALL PRIVILEGES ON `elasticrelay`.* TO 'elasticrelay_user'@'%';

-- 刷新权限
FLUSH PRIVILEGES;

-- 使用elasticrelay数据库
USE elasticrelay;

-- 创建测试表
CREATE TABLE IF NOT EXISTS mysql_users (
    id INT AUTO_INCREMENT PRIMARY KEY,
    username VARCHAR(100) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    phone VARCHAR(20),
    birth_date DATE,
    profile JSON,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    is_verified BOOLEAN DEFAULT FALSE,
    last_login TIMESTAMP NULL,
    INDEX idx_email (email),
    INDEX idx_created_at (created_at),
    INDEX idx_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS mysql_orders (
    id INT AUTO_INCREMENT PRIMARY KEY,
    user_id INT,
    order_number VARCHAR(50) UNIQUE NOT NULL,
    total_amount DECIMAL(10,2) NOT NULL,
    status ENUM('pending', 'processing', 'shipped', 'delivered', 'cancelled') DEFAULT 'pending',
    order_data JSON,
    shipping_address TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    shipped_at TIMESTAMP NULL,
    delivered_at TIMESTAMP NULL,
    FOREIGN KEY (user_id) REFERENCES mysql_users(id) ON DELETE SET NULL,
    INDEX idx_user_id (user_id),
    INDEX idx_created_at (created_at),
    INDEX idx_status (status),
    INDEX idx_order_number (order_number)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS mysql_products (
    id INT AUTO_INCREMENT PRIMARY KEY,
    sku VARCHAR(100) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    price DECIMAL(10,2) NOT NULL,
    category VARCHAR(100),
    specifications JSON,
    in_stock BOOLEAN DEFAULT TRUE,
    stock_quantity INT DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_category (category),
    INDEX idx_sku (sku),
    INDEX idx_name (name),
    INDEX idx_price (price)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS test_table (
    id INT AUTO_INCREMENT PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE,
    age INT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    metadata JSON,
    is_active BOOLEAN DEFAULT TRUE,
    INDEX idx_created_at (created_at),
    INDEX idx_email (email),
    INDEX idx_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 插入测试数据
INSERT IGNORE INTO mysql_users (username, email, first_name, last_name, phone, profile) VALUES 
('zhangsan_mysql', 'zhangsan.mysql@test.com', '三', '张', '13800138001', JSON_OBJECT('level', 'vip', 'points', 1000, 'preferences', JSON_ARRAY('tech', 'gaming'))),
('lisi_mysql', 'lisi.mysql@test.com', '四', '李', '13800138002', JSON_OBJECT('level', 'premium', 'points', 2500, 'preferences', JSON_ARRAY('fashion', 'travel'))),
('wangwu_mysql', 'wangwu.mysql@test.com', '五', '王', '13800138003', JSON_OBJECT('level', 'basic', 'points', 100, 'preferences', JSON_ARRAY('sports', 'music')));

INSERT IGNORE INTO mysql_products (sku, name, description, price, category, specifications, stock_quantity) VALUES 
('MYSQL001', 'iPhone 15 Pro (MySQL)', 'Apple iPhone 15 Pro 256GB - MySQL Test', 8999.00, 'electronics', JSON_OBJECT('color', 'titanium', 'storage', '256GB', 'warranty', '1year'), 50),
('MYSQL002', 'MacBook Pro (MySQL)', 'Apple MacBook Pro 14inch M3 - MySQL Test', 15999.00, 'computers', JSON_OBJECT('processor', 'M3', 'ram', '16GB', 'storage', '512GB'), 20),
('MYSQL003', 'AirPods Pro (MySQL)', 'Apple AirPods Pro 3rd Gen - MySQL Test', 1899.00, 'accessories', JSON_OBJECT('noise_cancelling', true, 'battery_life', '6h', 'color', 'white'), 100);

INSERT IGNORE INTO test_table (name, email, age, metadata) VALUES 
('MySQL张三', 'mysql.zhangsan@example.com', 25, JSON_OBJECT('city', '北京', 'department', 'MySQL技术部', 'database', 'mysql')),
('MySQL李四', 'mysql.lisi@example.com', 30, JSON_OBJECT('city', '上海', 'department', 'MySQL产品部', 'database', 'mysql')),
('MySQL王五', 'mysql.wangwu@example.com', 28, JSON_OBJECT('city', '广州', 'department', 'MySQL运营部', 'database', 'mysql'));

-- 插入订单数据（引用用户）
INSERT IGNORE INTO mysql_orders (user_id, order_number, total_amount, status, order_data, shipping_address) 
SELECT 
    u.id,
    CONCAT('ORD-MYSQL-', LPAD(u.id, 6, '0')),
    8999.00,
    'pending',
    JSON_OBJECT(
        'items', JSON_ARRAY(
            JSON_OBJECT('sku', 'MYSQL001', 'quantity', 1, 'price', 8999.00)
        ),
        'payment_method', 'credit_card',
        'notes', 'MySQL CDC测试订单'
    ),
    CONCAT('MySQL测试地址 ', u.id, '号')
FROM mysql_users u
WHERE u.username LIKE '%mysql%'
LIMIT 3;

-- 创建存储过程用于测试CDC
DELIMITER //
CREATE PROCEDURE IF NOT EXISTS GenerateTestData(IN record_count INT)
BEGIN
    DECLARE i INT DEFAULT 1;
    DECLARE random_name VARCHAR(100);
    DECLARE random_email VARCHAR(255);
    
    WHILE i <= record_count DO
        SET random_name = CONCAT('TestUser_', i, '_', UNIX_TIMESTAMP());
        SET random_email = CONCAT('test_', i, '_', UNIX_TIMESTAMP(), '@mysql-cdc.com');
        
        INSERT INTO test_table (name, email, age, metadata) VALUES (
            random_name,
            random_email,
            FLOOR(RAND() * 50) + 18,
            JSON_OBJECT(
                'batch', 'generated',
                'sequence', i,
                'timestamp', UNIX_TIMESTAMP(),
                'database', 'mysql'
            )
        );
        
        SET i = i + 1;
    END WHILE;
END//
DELIMITER ;

-- 显示初始化完成信息
SELECT 'MySQL database initialized successfully for ElasticRelay CDC' as message;
SELECT 'Created user: elasticrelay_user' as message;
SELECT 'Created tables: mysql_users, mysql_orders, mysql_products, test_table' as message;
SELECT 'Inserted sample data' as message;
SELECT 'Ready for binlog CDC setup' as message;

-- 显示表统计信息
SELECT 
    TABLE_NAME,
    TABLE_ROWS,
    CREATE_TIME
FROM information_schema.TABLES 
WHERE TABLE_SCHEMA = 'elasticrelay' 
    AND TABLE_NAME IN ('mysql_users', 'mysql_orders', 'mysql_products', 'test_table');

-- 显示binlog状态
SHOW VARIABLES LIKE 'log_bin';
SHOW VARIABLES LIKE 'binlog_format';
SHOW VARIABLES LIKE 'server_id';
