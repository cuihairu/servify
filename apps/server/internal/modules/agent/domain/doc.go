// Package domain defines the core agent business entities.
//
// Agent owns profile, skills, status, concurrency limits and load
// snapshot (V1.0 convergence, see docs/v1-convergence-plan.md §2.1);
// shift schedules feed the availability factor, and the routing scorer
// consumes these to rank candidate agents for a conversation.
package domain