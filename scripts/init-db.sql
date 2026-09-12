-- 数据库初始化脚本
--
-- DEPRECATED: 本脚本已由版本化迁移取代（apps/server/internal/app/bootstrap/
-- migrations/000001_init.up.sql，经 cmd/migrate / server 启动自动应用）。
-- compose 编排不再挂载本文件；仅为手动初始化裸库保留，内容与迁移基线保持
-- 语义一致，新增 schema 变更一律写进 migrations/000NNN_*.up.sql，勿改此处。

-- 创建 pgvector 扩展
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS hstore;

-- 创建 WeKnora compatibility 与 Servify 的映射表
CREATE TABLE IF NOT EXISTS servify_weknora_mappings (
    id SERIAL PRIMARY KEY,
    servify_doc_id INTEGER,
    weknora_doc_id VARCHAR(255) NOT NULL,
    weknora_kb_id VARCHAR(255) NOT NULL,
    mapping_type VARCHAR(50) DEFAULT 'document',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(servify_doc_id, weknora_doc_id)
);

-- 创建索引优化查询
CREATE INDEX IF NOT EXISTS idx_mappings_servify_doc ON servify_weknora_mappings(servify_doc_id);
CREATE INDEX IF NOT EXISTS idx_mappings_weknora_doc ON servify_weknora_mappings(weknora_doc_id);
CREATE INDEX IF NOT EXISTS idx_mappings_kb ON servify_weknora_mappings(weknora_kb_id);

-- 创建 AI 服务监控表
CREATE TABLE IF NOT EXISTS ai_service_metrics (
    id SERIAL PRIMARY KEY,
    service_type VARCHAR(50) NOT NULL, -- compatibility values include 'weknora', plus 'fallback' and 'openai'
    query_count BIGINT DEFAULT 0,
    success_count BIGINT DEFAULT 0,
    error_count BIGINT DEFAULT 0,
    avg_latency_ms INTEGER DEFAULT 0,
    last_updated TIMESTAMP DEFAULT NOW(),
    date DATE DEFAULT CURRENT_DATE,
    UNIQUE(service_type, date)
);

-- 创建知识库同步日志表
CREATE TABLE IF NOT EXISTS knowledge_sync_logs (
    id SERIAL PRIMARY KEY,
    operation VARCHAR(50) NOT NULL, -- 'upload', 'update', 'delete'
    document_id VARCHAR(255),
    servify_doc_id INTEGER,
    weknora_doc_id VARCHAR(255),
    status VARCHAR(50) NOT NULL, -- 'pending', 'success', 'failed'
    error_message TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    completed_at TIMESTAMP
);

-- 插入初始指标数据
INSERT INTO ai_service_metrics (service_type, date)
VALUES
    ('weknora', CURRENT_DATE),
    ('fallback', CURRENT_DATE),
    ('openai', CURRENT_DATE)
ON CONFLICT (service_type, date) DO NOTHING;

-- 创建更新时间戳的函数
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- 创建触发器
CREATE TRIGGER update_mappings_updated_at
    BEFORE UPDATE ON servify_weknora_mappings
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Note: Sample data for knowledge_docs should be inserted via API after tables are created by AutoMigrate
-- The following is commented out because knowledge_docs table is created by GORM AutoMigrate
-- INSERT INTO knowledge_docs (title, content, category, tags, created_at, updated_at)
-- VALUES
--     ('产品安装指南', '本指南详细介绍了产品的安装步骤...', '技术支持', '安装,指南,技术', NOW(), NOW()),
--     ('常见问题解答', '以下是用户最常遇到的问题及解决方案...', '常见问题', '问题,解答,FAQ', NOW(), NOW()),
--     ('API 使用文档', 'REST API 接口说明和使用示例...', '开发文档', 'API,开发,文档', NOW(), NOW())
-- ON CONFLICT DO NOTHING;

-- 扩展 knowledge_docs 表以支持 pgvector
ALTER TABLE knowledge_docs
ADD COLUMN IF NOT EXISTS embedding vector(1536),
ADD COLUMN IF NOT EXISTS chunk_index int DEFAULT 0,
ADD COLUMN IF NOT EXISTS doc_chunk_id varchar(255);

-- 创建向量索引（使用 ivfflat）
CREATE INDEX IF NOT EXISTS idx_knowledge_docs_embedding
ON knowledge_docs
USING ivfflat (embedding vector_cosine_ops)
WITH (lists = 100);

-- 为 doc_chunk_id 创建索引以优化 chunk 查询
CREATE INDEX IF NOT EXISTS idx_knowledge_docs_doc_chunk_id
ON knowledge_docs(doc_chunk_id);

-- 添加注释
COMMENT ON COLUMN knowledge_docs.embedding IS '文档的向量表示，用于语义搜索';
COMMENT ON COLUMN knowledge_docs.chunk_index IS '文档分块索引，同一文档的不同分块有相同 doc_chunk_id';
COMMENT ON COLUMN knowledge_docs.doc_chunk_id IS '文档块 ID，用于标识属于同一文档的不同分块';
