#!/bin/bash
# 修复 PostgreSQL publication 配置

echo "==================================="
echo "修复 ElasticRelay Publication"
echo "==================================="

PGPASSWORD=postgres psql -h 127.0.0.1 -p 5432 -U postgres -d elasticrelay << 'EOF'
-- 1. 查看当前 publication 状态
\echo '=== 当前 publication 包含的表 ==='
SELECT schemaname, tablename 
FROM pg_publication_tables 
WHERE pubname = 'elasticrelay_publication' 
ORDER BY schemaname, tablename;

-- 2. 添加表到 publication（如果表已在 publication 中会报错，可以忽略）
\echo ''
\echo '=== 添加表到 publication ==='
DO $$ 
DECLARE
    table_name text;
    table_list text[] := ARRAY['test_table', 'users', 'orders', 'products'];
BEGIN
    FOREACH table_name IN ARRAY table_list
    LOOP
        BEGIN
            EXECUTE format('ALTER PUBLICATION elasticrelay_publication ADD TABLE %I', table_name);
            RAISE NOTICE '已添加表: %', table_name;
        EXCEPTION 
            WHEN duplicate_object THEN
                RAISE NOTICE '表 % 已在 publication 中', table_name;
            WHEN undefined_table THEN
                RAISE WARNING '表 % 不存在，跳过', table_name;
        END;
    END LOOP;
END $$;

-- 3. 设置 REPLICA IDENTITY（确保能捕获完整数据变更）
\echo ''
\echo '=== 设置 REPLICA IDENTITY ==='
DO $$ 
DECLARE
    table_name text;
    table_list text[] := ARRAY['test_table', 'users', 'orders', 'products'];
BEGIN
    FOREACH table_name IN ARRAY table_list
    LOOP
        BEGIN
            EXECUTE format('ALTER TABLE %I REPLICA IDENTITY FULL', table_name);
            RAISE NOTICE '已设置 %% REPLICA IDENTITY FULL', table_name;
        EXCEPTION 
            WHEN undefined_table THEN
                RAISE WARNING '表 % 不存在，跳过', table_name;
        END;
    END LOOP;
END $$;

-- 4. 验证最终结果
\echo ''
\echo '=== 最终 publication 配置 ==='
SELECT schemaname, tablename 
FROM pg_publication_tables 
WHERE pubname = 'elasticrelay_publication' 
ORDER BY schemaname, tablename;

\echo ''
\echo '=== 表的 REPLICA IDENTITY 设置 ==='
SELECT 
    schemaname || '.' || tablename AS full_table_name,
    CASE relreplident
        WHEN 'd' THEN 'DEFAULT'
        WHEN 'n' THEN 'NOTHING'
        WHEN 'f' THEN 'FULL'
        WHEN 'i' THEN 'INDEX'
    END AS replica_identity
FROM pg_publication_tables pt
JOIN pg_class c ON c.relname = pt.tablename
JOIN pg_namespace n ON n.oid = c.relnamespace AND n.nspname = pt.schemaname
WHERE pt.pubname = 'elasticrelay_publication'
ORDER BY schemaname, tablename;

EOF

echo ""
echo "==================================="
echo "修复完成！现在可以重启 ElasticRelay"
echo "==================================="

