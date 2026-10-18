-- 000025: explicit tenant/workspace knowledge bases and ordered document chunks.
-- Preserve every existing document by assigning it to a default scoped base and
-- copying its current row into chunk 0 before new code relies on the child table.
CREATE TABLE IF NOT EXISTS "knowledge_bases" (
  id bigserial PRIMARY KEY,
  tenant_id text NOT NULL,
  workspace_id text NOT NULL,
  name text NOT NULL,
  is_public boolean NOT NULL DEFAULT false,
  created_at timestamptz,
  updated_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_knowledge_bases_tenant_id ON knowledge_bases(tenant_id);
CREATE INDEX IF NOT EXISTS idx_knowledge_bases_workspace_id ON knowledge_bases(workspace_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_knowledge_bases_scope_name ON knowledge_bases(tenant_id, workspace_id, name);

ALTER TABLE knowledge_docs ADD COLUMN IF NOT EXISTS knowledge_base_id bigint;
INSERT INTO knowledge_bases (tenant_id, workspace_id, name, created_at, updated_at)
SELECT DISTINCT d.tenant_id, d.workspace_id, 'Default', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
FROM knowledge_docs d
WHERE NOT EXISTS (
  SELECT 1 FROM knowledge_bases b
  WHERE b.tenant_id = d.tenant_id AND b.workspace_id = d.workspace_id AND b.name = 'Default'
);
UPDATE knowledge_docs d SET knowledge_base_id = b.id
FROM knowledge_bases b
WHERE d.knowledge_base_id IS NULL AND b.tenant_id = d.tenant_id AND b.workspace_id = d.workspace_id AND b.name = 'Default';
CREATE INDEX IF NOT EXISTS idx_knowledge_docs_knowledge_base_id ON knowledge_docs(knowledge_base_id);
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_knowledge_docs_knowledge_base') THEN
    ALTER TABLE knowledge_docs ADD CONSTRAINT fk_knowledge_docs_knowledge_base
      FOREIGN KEY (knowledge_base_id) REFERENCES knowledge_bases(id) ON DELETE SET NULL;
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS "knowledge_chunks" (
  id bigserial PRIMARY KEY,
  document_id bigint NOT NULL REFERENCES knowledge_docs(id) ON DELETE CASCADE,
  sequence bigint NOT NULL,
  content text NOT NULL,
  embedding vector(1536),
  created_at timestamptz
);
CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_document_id ON knowledge_chunks(document_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_knowledge_chunks_document_sequence ON knowledge_chunks(document_id, sequence);
INSERT INTO knowledge_chunks (document_id, sequence, content, embedding, created_at)
SELECT id, chunk_index, COALESCE(content, ''), embedding, created_at
FROM knowledge_docs
ON CONFLICT (document_id, sequence) DO NOTHING;
