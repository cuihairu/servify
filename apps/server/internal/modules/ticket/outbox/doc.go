// Package outbox implements the ticket reliable-event outbox: ticket rows,
// initial status records and reliable events commit in one DB transaction,
// and a worker asynchronously claims pending rows and publishes them to the
// event bus exactly once (deduplicated by event_id).
package outbox
