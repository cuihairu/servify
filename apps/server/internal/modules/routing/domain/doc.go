// Package domain contains routing module entities and value objects.
//
// Routing is the assignment mechanism of a conversation's service
// process (V1.0 convergence, see docs/v1-convergence-plan.md §6): it
// owns the waiting queue, assignment, transfer workflows, skill-based
// routing and escalation routing. Scores and factors are persisted to
// routing_assignments for audit and analytics projection.
package domain