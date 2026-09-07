package domain

import "testing"

func TestAgentLoadCanTakeChat(t *testing.T) {
	under := AgentLoad{UserID: 1, MaxChatConcurrency: 3, CurrentChatLoad: 2}
	if !under.CanTakeChat() {
		t.Fatal("expected load under max to accept chat")
	}
	atMax := AgentLoad{UserID: 1, MaxChatConcurrency: 3, CurrentChatLoad: 3}
	if atMax.CanTakeChat() {
		t.Fatal("expected load at max to refuse chat")
	}
}
