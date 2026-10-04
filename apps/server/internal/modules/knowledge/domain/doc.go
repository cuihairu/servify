// Package domain contains knowledge module entities and value objects,
// plus the module-owned GORM persistence models (KnowledgeDoc /
// KnowledgeIndexJob and the pgvector column wrapper Embedding, see
// models.go / embedding.go — migrated from internal/models during P1-7).
//
// Knowledge is a horizontal capability for the whole service process
// (V1.0 convergence, see docs/v1-convergence-plan.md §8): all access
// goes through the KnowledgeProvider abstraction so AI, Agent and
// Ticket consume retrieval uniformly, independent of the concrete
// provider (pgvector / Dify / WeKnora / RAGFlow / future).
package domain
