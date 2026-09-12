package application

// v1 可订阅事件白名单：只收录 eventbus 上真实存在 publisher 的事件。
// ticket.updated / sla.violation 目前走 automation 直调通道、不经 bus，列为保留名暂不收录。
const (
	EventTicketCreated               = "ticket.created"
	EventTicketAssigned              = "ticket.assigned"
	EventTicketClosed                = "ticket.closed"
	EventConversationCreated         = "conversation.created"
	EventConversationMessageReceived = "conversation.message_received"
	EventRoutingAgentAssigned        = "routing.agent_assigned"
	EventRoutingTransferCompleted    = "routing.transfer_completed"
)

// SupportedEvents 返回可订阅事件白名单（管理 API 校验与管理端下拉共用）。
func SupportedEvents() []string {
	return []string{
		EventTicketCreated,
		EventTicketAssigned,
		EventTicketClosed,
		EventConversationCreated,
		EventConversationMessageReceived,
		EventRoutingAgentAssigned,
		EventRoutingTransferCompleted,
	}
}

// IsSupportedEvent 判断事件名是否在白名单内。
func IsSupportedEvent(name string) bool {
	for _, candidate := range SupportedEvents() {
		if candidate == name {
			return true
		}
	}
	return false
}
