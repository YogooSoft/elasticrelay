-- PostgreSQL 初始化脚本用于ElasticRelay CDC
-- 创建用户、数据库和必要的权限设置

-- 创建ElasticRelay专用用户
DO
$do$
BEGIN
   IF NOT EXISTS (
      SELECT FROM pg_catalog.pg_roles
      WHERE  rolname = 'elasticrelay_user') THEN
      
      CREATE USER elasticrelay_user WITH 
        LOGIN 
        PASSWORD 'elasticrelay_pass'
        REPLICATION;
   END IF;
END
$do$;

-- 赋予必要的权限
GRANT CONNECT ON DATABASE elasticrelay TO elasticrelay_user;
GRANT USAGE ON SCHEMA public TO elasticrelay_user;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO elasticrelay_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO elasticrelay_user;

-- 创建测试表
CREATE TABLE IF NOT EXISTS test_table (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE,
    age INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    metadata JSONB,
    tags TEXT[],
    is_active BOOLEAN DEFAULT true
);

CREATE TABLE IF NOT EXISTS users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(100) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL UNIQUE,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    phone VARCHAR(20),
    birth_date DATE,
    profile JSONB,
    preferences TEXT[],
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    is_verified BOOLEAN DEFAULT false,
    last_login TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    order_number VARCHAR(50) UNIQUE NOT NULL,
    total_amount DECIMAL(10,2) NOT NULL,
    status VARCHAR(20) DEFAULT 'pending',
    order_data JSONB,
    shipping_address TEXT,
    order_items TEXT[],
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    shipped_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS products (
    id SERIAL PRIMARY KEY,
    sku VARCHAR(100) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    price DECIMAL(10,2) NOT NULL,
    category VARCHAR(100),
    specifications JSONB,
    tags TEXT[],
    in_stock BOOLEAN DEFAULT true,
    stock_quantity INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- 插入测试数据
INSERT INTO test_table (name, email, age, metadata, tags) VALUES 
('张三', 'zhangsan@example.com', 25, '{"city": "北京", "department": "技术部"}', ARRAY['golang', 'postgresql']),
('李四', 'lisi@example.com', 30, '{"city": "上海", "department": "产品部"}', ARRAY['product', 'design']),
('王五', 'wangwu@example.com', 28, '{"city": "广州", "department": "运营部"}', ARRAY['marketing', 'data'])
ON CONFLICT (email) DO NOTHING;

INSERT INTO users (username, email, first_name, last_name, phone, profile, preferences) VALUES 
('zhangsan', 'zhangsan@test.com', '三', '张', '13800138001', '{"level": "vip", "points": 1000}', ARRAY['tech', 'gaming']),
('lisi', 'lisi@test.com', '四', '李', '13800138002', '{"level": "premium", "points": 2500}', ARRAY['fashion', 'travel']),
('wangwu', 'wangwu@test.com', '五', '王', '13800138003', '{"level": "basic", "points": 100}', ARRAY['sports', 'music'])
ON CONFLICT (email) DO NOTHING;

INSERT INTO products (sku, name, description, price, category, specifications, tags, stock_quantity) VALUES 
('SKU001', 'iPhone 15 Pro', 'Apple iPhone 15 Pro 256GB', 8999.00, 'electronics', '{"color": "titanium", "storage": "256GB"}', ARRAY['phone', 'apple', 'flagship'], 50),
('SKU002', 'MacBook Pro', 'Apple MacBook Pro 14inch M3', 15999.00, 'computers', '{"processor": "M3", "ram": "16GB"}', ARRAY['laptop', 'apple', 'professional'], 20),
('SKU003', 'AirPods Pro', 'Apple AirPods Pro 3rd Gen', 1899.00, 'accessories', '{"noise_cancelling": true, "battery_life": "6h"}', ARRAY['headphones', 'wireless', 'apple'], 100)
ON CONFLICT (sku) DO NOTHING;

-- 创建索引优化性能
CREATE INDEX IF NOT EXISTS idx_test_table_created_at ON test_table(created_at);
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_created_at ON users(created_at);
CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id);
CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at);
CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_products_category ON products(category);
CREATE INDEX IF NOT EXISTS idx_products_sku ON products(sku);

-- 创建更新时间触发器函数
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- 为需要的表创建更新触发器
DO $$ 
BEGIN
    -- test_table
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'update_test_table_updated_at') THEN
        CREATE TRIGGER update_test_table_updated_at BEFORE UPDATE ON test_table 
        FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
    END IF;
    
    -- users 
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'update_users_updated_at') THEN
        CREATE TRIGGER update_users_updated_at BEFORE UPDATE ON users 
        FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
    END IF;
    
    -- orders
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'update_orders_updated_at') THEN
        CREATE TRIGGER update_orders_updated_at BEFORE UPDATE ON orders 
        FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
    END IF;
    
    -- products
    IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'update_products_updated_at') THEN
        CREATE TRIGGER update_products_updated_at BEFORE UPDATE ON products 
        FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
    END IF;
END $$;

-- 输出初始化完成信息
DO $$ 
BEGIN
    RAISE NOTICE 'PostgreSQL database initialized successfully for ElasticRelay CDC';
    RAISE NOTICE 'Created user: elasticrelay_user';
    RAISE NOTICE 'Created tables: test_table, users, orders, products';
    RAISE NOTICE 'Inserted sample data and configured triggers';
    RAISE NOTICE 'Ready for logical replication setup';
END $$;
