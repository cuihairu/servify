// Package domain contains ticket module entities and value objects.
//
// Ticket is a follow-up work item derived from a Conversation, not a
// peer center object (V1.0 convergence, see docs/v1-convergence-plan.md
// §7 and ARCHITECTURE.md §6.6): a new ticket must carry conversation_id,
// and sla / satisfaction / custom_field semantics belong to the ticket
// capability. Ticket domain events are projected into conversation_events
// so the customer-facing timeline stays complete.
package domain