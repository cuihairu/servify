package server

import (
	"context"

	"servify/apps/server/internal/config"
	aidelivery "servify/apps/server/internal/modules/ai/delivery"
	conversationapp "servify/apps/server/internal/modules/conversation/application"
	conversationdelivery "servify/apps/server/internal/modules/conversation/delivery"
	conversationinfra "servify/apps/server/internal/modules/conversation/infra"
	realtimeplatform "servify/apps/server/internal/platform/realtime"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// RealtimeRuntime assembles realtime and AI-facing primitives used by lightweight runtimes.
type RealtimeRuntime struct {
	Config           *config.Config
	Logger           *logrus.Logger
	DB               *gorm.DB
	AIService        aidelivery.RuntimeService
	AIHandlerService aidelivery.HandlerService
	wsRuntime        websocketRunner
	RealtimeGateway  realtimeplatform.RealtimeGateway
	RTCGateway       realtimeplatform.RTCGateway
	MessageRouter    realtimeplatform.MessageRouterRuntime
}

func BuildRealtimeRuntime(cfg *config.Config, logger *logrus.Logger, db *gorm.DB, ai aidelivery.RuntimeService, handlerAI aidelivery.HandlerService) *RealtimeRuntime {
	wsHub := realtimeplatform.NewWebSocketHub()
	if db != nil {
		conversationRepo := conversationinfra.NewGormRepository(db)
		conversationService := conversationapp.NewService(conversationRepo, nil)
		wsHub.SetConversationMessageWriter(conversationdelivery.NewWebSocketMessageAdapter(conversationService))
	}
	wsHub.SetAIService(ai)

	webrtcService := realtimeplatform.NewWebRTCService(cfg.WebRTC.STUNServer, wsHub)
	wsHub.SetWebRTCService(webrtcService)
	return &RealtimeRuntime{
		Config:           cfg,
		Logger:           logger,
		DB:               db,
		AIService:        ai,
		AIHandlerService: handlerAI,
		wsRuntime:        wsHub,
		RealtimeGateway:  realtimeplatform.NewWebSocketAdapter(wsHub),
		RTCGateway:       realtimeplatform.NewWebRTCAdapter(webrtcService),
		MessageRouter:    realtimeplatform.NewMessageRouter(ai, wsHub, db),
	}
}

func (rt *RealtimeRuntime) Start() error {
	if rt.wsRuntime != nil {
		go rt.wsRuntime.Run()
	}
	return rt.MessageRouter.Start()
}

func (rt *RealtimeRuntime) Stop(context.Context) error {
	if rt.MessageRouter == nil {
		return nil
	}
	return rt.MessageRouter.Stop()
}
