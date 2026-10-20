package worker

import (
	"context"

	"servify/apps/server/internal/platform/channel/chatconnector"
)

// chatConnectorWorker adapts the chat connector supervisor to the
// bootstrap.Worker shape: the supervisor's Start takes a context (it reads the
// enabled installation set), the worker lifecycle's Start does not, so the
// adapter supplies a background context and forwards Stop's shutdown deadline
// unchanged.
type chatConnectorWorker struct {
	supervisor *chatconnector.Supervisor
}

func (w *chatConnectorWorker) Name() string { return "chat-connector-poll" }

func (w *chatConnectorWorker) Start() error {
	return w.supervisor.Start(context.Background())
}

func (w *chatConnectorWorker) Stop(ctx context.Context) error {
	return w.supervisor.Stop(ctx)
}
