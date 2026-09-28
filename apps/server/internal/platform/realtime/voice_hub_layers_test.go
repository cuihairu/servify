package realtime

import (
	"testing"
	"time"
)

// TestVoiceTeardownFullSendDropsFinalFrame 收线时客户端 Send 已满（无人
// 消费）：最后下行帧走非阻塞 default 丢弃，随后照常 close + 摘除会话表
// ——丢弃的是帧，收线语义不变。
func TestVoiceTeardownFullSendDropsFinalFrame(t *testing.T) {
	hub := NewVoiceHub()
	go hub.Run()

	client := &VoiceClient{
		ID:        "voice-full-send",
		SessionID: "s1",
		Send:      make(chan WebSocketMessage, 1),
		Hub:       hub,
	}
	// 预填满 Send 且本测试不消费。
	client.Send <- WebSocketMessage{Type: "translation-delta", SessionID: "s1"}
	hub.register <- client

	deadline := time.Now().Add(3 * time.Second)
	for {
		hub.mutex.RLock()
		_, ok := hub.clients[client.ID]
		hub.mutex.RUnlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("client never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	hub.teardown <- &voiceTeardown{
		client:  client,
		message: &WebSocketMessage{Type: "voice-error", Data: map[string]interface{}{"code": "stream_broken"}},
	}
	for {
		hub.mutex.RLock()
		_, ok := hub.clients[client.ID]
		hub.mutex.RUnlock()
		if !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("teardown never processed")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// 缓冲里只有预填帧；错误帧被丢弃；随后通道关闭。
	first := <-client.Send
	if first.Type != "translation-delta" {
		t.Fatalf("first buffered frame = %s, want the prefilled delta", first.Type)
	}
	if _, ok := <-client.Send; ok {
		t.Fatal("Send must be closed after teardown and must not contain the dropped frame")
	}
}

// TestVoiceFrameSinkEmitDropsWhenBroadcastFull 广播通道满（缓冲 256 无
// 消费方）：voiceFrameSink.emit 非阻塞丢帧直接返回，管线不因慢下行停摆。
func TestVoiceFrameSinkEmitDropsWhenBroadcastFull(t *testing.T) {
	hub := NewVoiceHub() // 不起 Run：broadcast 无消费方，保持满载
	sink := &voiceFrameSink{client: &VoiceClient{Hub: hub, SessionID: "s1"}}

	for i := 0; i < cap(hub.broadcast); i++ {
		hub.broadcast <- WebSocketMessage{Type: "filler"}
	}

	done := make(chan struct{})
	go func() {
		sink.emit("translation-delta", map[string]interface{}{"seq": 1})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("emit must not block on a full broadcast channel")
	}
	if len(hub.broadcast) != cap(hub.broadcast) {
		t.Fatalf("broadcast len = %d, want full (frame must be dropped)", len(hub.broadcast))
	}
}
