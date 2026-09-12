package delivery

import (
	"context"

	"servify/apps/server/internal/modules/webhook/application"
	"servify/apps/server/internal/platform/eventbus"
)

// EventBusSubscriber 把白名单内的事件接到 webhook 服务。
// handler 在 bus 发布方 goroutine 内同步执行，因此只允许入队（回查+落行），
// HTTP 投递全部由后台 worker 完成。
type EventBusSubscriber struct {
	service *application.Service
}

func NewEventBusSubscriber(service *application.Service) *EventBusSubscriber {
	return &EventBusSubscriber{service: service}
}

func (s *EventBusSubscriber) Register(bus eventbus.Bus) {
	if bus == nil || s == nil || s.service == nil {
		return
	}
	for _, name := range application.SupportedEvents() {
		eventName := name
		bus.Subscribe(eventName, eventbus.HandlerFunc(func(ctx context.Context, evt eventbus.Event) error {
			s.service.EnqueueEvent(ctx, eventName, evt.AggregateID(), evt.ID())
			return nil
		}))
	}
}
