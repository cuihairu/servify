package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKnowledgeDoc_PgvectorFields(t *testing.T) {
	doc := KnowledgeDoc{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		ProviderID:  "pgvector",
		ExternalID:  "doc-123",
		Title:       "Test Document",
		Content:     "This is a test content for knowledge base.",
		Category:    "technical",
		IsPublic:    true,
		Embedding:   NewEmbedding([]float32{0.1, 0.2, 0.3}),
		ChunkIndex:  1,
		DocChunkID:  "doc-123-chunk-1",
	}

	assert.Equal(t, "tenant-1", doc.TenantID)
	assert.Equal(t, "workspace-1", doc.WorkspaceID)
	assert.Equal(t, "pgvector", doc.ProviderID)
	assert.Equal(t, "doc-123", doc.ExternalID)
	assert.Equal(t, "Test Document", doc.Title)
	assert.Equal(t, "technical", doc.Category)
	assert.True(t, doc.IsPublic)
	assert.NotNil(t, doc.Embedding)
	assert.Equal(t, 1, doc.ChunkIndex)
	assert.Equal(t, "doc-123-chunk-1", doc.DocChunkID)
}

func TestKnowledgeDoc_EmptyEmbedding(t *testing.T) {
	doc := KnowledgeDoc{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		ProviderID:  "pgvector",
		ExternalID:  "doc-456",
		Title:       "Document without embedding",
	}

	// pgvector.Vector is a struct, not a pointer, so check if underlying slice is nil
	assert.Nil(t, doc.Embedding.Slice())
	assert.Equal(t, 0, doc.ChunkIndex)
	assert.Empty(t, doc.DocChunkID)
}

func TestKnowledgeDoc_Vector1536Dimensions(t *testing.T) {
	// Test creating a vector with 1536 dimensions (OpenAI embedding size)
	vector := make([]float32, 1536)
	for i := range vector {
		vector[i] = 0.1
	}

	doc := KnowledgeDoc{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		ProviderID:  "pgvector",
		ExternalID:  "doc-789",
		Title:       "Document with full embedding",
		Embedding:   NewEmbedding(vector),
		ChunkIndex:  0,
		DocChunkID:  "doc-789-chunk-0",
	}

	assert.NotNil(t, doc.Embedding)
	assert.Equal(t, 1536, len(doc.Embedding.Slice()))
	assert.Equal(t, 0, doc.ChunkIndex)
	assert.Equal(t, "doc-789-chunk-0", doc.DocChunkID)
}

func TestKnowledgeDoc_ChunkFields(t *testing.T) {
	// Test chunk-related fields for document chunking
	doc := KnowledgeDoc{
		TenantID:    "tenant-1",
		WorkspaceID: "workspace-1",
		ProviderID:  "pgvector",
		ExternalID:  "long-doc-123",
		Title:       "Long Document Chunk",
		Content:     "This is a chunk of a long document.",
		ChunkIndex:  5,
		DocChunkID:  "long-doc-123-chunk-5",
	}

	assert.Equal(t, 5, doc.ChunkIndex)
	assert.Equal(t, "long-doc-123-chunk-5", doc.DocChunkID)
	assert.Equal(t, "long-doc-123", doc.ExternalID)
}
