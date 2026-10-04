// Package domain contains conversation module entities and value objects.
//
// Conversation is the single core aggregate of the service process
// (V1.0 convergence, see docs/v1-convergence-plan.md §2.1). It owns:
//
//   - the unified conversation record
//   - participants
//   - messages
//   - channel bindings
//   - conversation status
//
// Customer is the center of the "customer", Conversation is the center
// of the "service process". AI, Agent, Routing and Ticket are branches
// of a conversation's service process, never peers; other modules access
// conversations only through the conversation module application port
// or domain events (ARCHITECTURE.md §6.4).
package domain