-- Chinese text has no built-in PostgreSQL tokenizer. pg_trgm provides
-- character-trigram similarity for the knowledge provider's keyword path.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_knowledge_docs_text_trgm
ON knowledge_docs USING gin ((coalesce(title, '') || ' ' || coalesce(content, '')) gin_trgm_ops);
