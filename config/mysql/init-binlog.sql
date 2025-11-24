-- MySQL Binlog CDC 配置脚本
-- 配置MySQL用于CDC的binlog相关设置

-- 检查并显示binlog相关配置
SELECT 'Checking MySQL Binlog Configuration for CDC' as message;

-- 显示关键的binlog配置
SHOW VARIABLES LIKE 'log_bin';
SHOW VARIABLES LIKE 'binlog_format';
SHOW VARIABLES LIKE 'binlog_row_image';
SHOW VARIABLES LIKE 'server_id';
SHOW VARIABLES LIKE 'gtid_mode';
SHOW VARIABLES LIKE 'enforce_gtid_consistency';

-- 显示当前binlog文件信息
SHOW MASTER STATUS;

-- 显示binlog文件列表
SHOW BINARY LOGS;

-- 创建binlog监控视图
CREATE OR REPLACE VIEW binlog_status AS
SELECT 
    'Binlog Enabled' as status,
    @@log_bin as log_bin_enabled,
    @@binlog_format as binlog_format,
    @@binlog_row_image as binlog_row_image,
    @@server_id as server_id,
    @@gtid_mode as gtid_mode,
    @@enforce_gtid_consistency as enforce_gtid_consistency;

-- 显示用户权限信息
SELECT 
    User,
    Host,
    Repl_slave_priv,
    Repl_client_priv
FROM mysql.user 
WHERE User = 'elasticrelay_user';

-- 创建CDC测试函数
DELIMITER //
CREATE FUNCTION IF NOT EXISTS test_cdc_insert() RETURNS INT
READS SQL DATA
BEGIN
    DECLARE new_id INT;
    
    INSERT INTO test_table (name, email, age, metadata) VALUES (
        CONCAT('CDC_Test_', UNIX_TIMESTAMP()),
        CONCAT('cdc_test_', UNIX_TIMESTAMP(), '@test.com'),
        FLOOR(RAND() * 50) + 18,
        JSON_OBJECT(
            'test_type', 'cdc_function',
            'timestamp', NOW(),
            'binlog_file', (SELECT File FROM (SHOW MASTER STATUS) as ms LIMIT 1),
            'database', 'mysql'
        )
    );
    
    SET new_id = LAST_INSERT_ID();
    RETURN new_id;
END//
DELIMITER ;

-- 创建CDC测试存储过程 - 批量操作测试
DELIMITER //
CREATE PROCEDURE IF NOT EXISTS test_cdc_batch_operations()
BEGIN
    DECLARE done INT DEFAULT FALSE;
    DECLARE user_id INT;
    DECLARE cur CURSOR FOR SELECT id FROM mysql_users LIMIT 3;
    DECLARE CONTINUE HANDLER FOR NOT FOUND SET done = TRUE;

    -- 声明变量
    DECLARE test_order_id INT;
    
    SELECT 'Starting CDC batch operations test' as message;
    
    -- 插入测试
    INSERT INTO test_table (name, email, age, metadata) VALUES (
        'CDC_Batch_Insert',
        CONCAT('cdc_batch_', UNIX_TIMESTAMP(), '@test.com'),
        30,
        JSON_OBJECT('operation', 'batch_insert', 'timestamp', NOW())
    );
    
    -- 更新测试
    UPDATE test_table 
    SET metadata = JSON_SET(metadata, '$.last_updated', NOW(), '$.operation', 'batch_update')
    WHERE name LIKE 'CDC_%' 
    LIMIT 5;
    
    -- 为每个用户创建订单（插入测试）
    OPEN cur;
    read_loop: LOOP
        FETCH cur INTO user_id;
        IF done THEN
            LEAVE read_loop;
        END IF;
        
        INSERT INTO mysql_orders (user_id, order_number, total_amount, status, order_data) VALUES (
            user_id,
            CONCAT('CDC-BATCH-', user_id, '-', UNIX_TIMESTAMP()),
            ROUND(RAND() * 1000 + 100, 2),
            'pending',
            JSON_OBJECT(
                'test_type', 'cdc_batch',
                'user_id', user_id,
                'created_by', 'batch_procedure'
            )
        );
    END LOOP;
    CLOSE cur;
    
    SELECT 'CDC batch operations test completed' as message;
    SELECT COUNT(*) as new_test_records FROM test_table WHERE name LIKE 'CDC_%';
    SELECT COUNT(*) as new_orders FROM mysql_orders WHERE order_number LIKE 'CDC-BATCH-%';
    
END//
DELIMITER ;

-- 创建CDC性能测试存储过程
DELIMITER //
CREATE PROCEDURE IF NOT EXISTS test_cdc_performance(IN batch_size INT, IN batch_count INT)
BEGIN
    DECLARE i INT DEFAULT 1;
    DECLARE j INT DEFAULT 1;
    DECLARE start_time TIMESTAMP DEFAULT NOW();
    
    SELECT CONCAT('Starting CDC performance test: ', batch_count, ' batches of ', batch_size, ' records each') as message;
    
    WHILE i <= batch_count DO
        SET j = 1;
        
        -- 开始事务
        START TRANSACTION;
        
        WHILE j <= batch_size DO
            INSERT INTO test_table (name, email, age, metadata) VALUES (
                CONCAT('Perf_Test_', i, '_', j),
                CONCAT('perf_', i, '_', j, '_', UNIX_TIMESTAMP(), '@test.com'),
                FLOOR(RAND() * 50) + 18,
                JSON_OBJECT(
                    'batch_id', i,
                    'record_id', j,
                    'test_type', 'performance',
                    'timestamp', NOW()
                )
            );
            SET j = j + 1;
        END WHILE;
        
        -- 提交事务
        COMMIT;
        
        -- 显示进度
        IF i % 10 = 0 THEN
            SELECT CONCAT('Completed batch ', i, ' of ', batch_count) as progress;
        END IF;
        
        SET i = i + 1;
    END WHILE;
    
    SELECT 
        CONCAT('CDC performance test completed in ', 
               TIMESTAMPDIFF(SECOND, start_time, NOW()), 
               ' seconds') as result;
    SELECT 
        CONCAT('Total records inserted: ', batch_count * batch_size) as summary;
        
END//
DELIMITER ;

-- 显示binlog配置验证结果
SELECT 
    CASE 
        WHEN @@log_bin = 1 THEN 'PASS'
        ELSE 'FAIL'
    END as binlog_enabled,
    CASE 
        WHEN @@binlog_format = 'ROW' THEN 'PASS'
        ELSE 'FAIL' 
    END as binlog_format_check,
    CASE
        WHEN @@server_id > 0 THEN 'PASS'
        ELSE 'FAIL'
    END as server_id_check,
    CASE
        WHEN @@gtid_mode = 'ON' THEN 'PASS'
        ELSE 'WARN'
    END as gtid_mode_check;

-- 创建CDC监控表
CREATE TABLE IF NOT EXISTS cdc_monitor (
    id INT AUTO_INCREMENT PRIMARY KEY,
    check_time TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    binlog_file VARCHAR(255),
    binlog_position BIGINT,
    gtid_executed TEXT,
    notes TEXT,
    INDEX idx_check_time (check_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 插入初始监控记录
INSERT INTO cdc_monitor (binlog_file, binlog_position, gtid_executed, notes)
SELECT 
    File as binlog_file,
    Position as binlog_position,
    @@gtid_executed as gtid_executed,
    'Initial CDC setup completed' as notes
FROM (SHOW MASTER STATUS) as master_status;

SELECT 'MySQL Binlog CDC configuration completed successfully!' as final_message;
SELECT 'Use the following procedures for testing:' as testing_info;
SELECT '  - CALL test_cdc_batch_operations();' as test_1;
SELECT '  - SELECT test_cdc_insert();' as test_2;
SELECT '  - CALL test_cdc_performance(100, 10);' as test_3;
