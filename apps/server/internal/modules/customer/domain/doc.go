// Package domain defines customer business entities.
//
// Customer is the center of the "customer" (V1.0 convergence, see
// docs/v1-convergence-plan.md §2.1): profile, source, tags, contact
// attributes and notes. Conversations hang off a customer, and the
// customer panel aggregates profile, history, tickets and timeline in
// the Agent Workspace.
package domain