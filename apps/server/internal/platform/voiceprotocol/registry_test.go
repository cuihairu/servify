package voiceprotocol

import (
	"context"
	"testing"
)

type testSignalingAdapter struct{ protocol Protocol }

func (a testSignalingAdapter) Name() string       { return string(a.protocol) }
func (a testSignalingAdapter) Protocol() Protocol { return a.protocol }
func (a testSignalingAdapter) MapInvite(context.Context, interface{}) (CallEvent, error) {
	return CallEvent{}, nil
}
func (a testSignalingAdapter) MapAnswer(context.Context, interface{}) (CallEvent, error) {
	return CallEvent{}, nil
}
func (a testSignalingAdapter) MapHold(context.Context, interface{}) (CallEvent, error) {
	return CallEvent{}, nil
}
func (a testSignalingAdapter) MapResume(context.Context, interface{}) (CallEvent, error) {
	return CallEvent{}, nil
}
func (a testSignalingAdapter) MapHangup(context.Context, interface{}) (CallEvent, error) {
	return CallEvent{}, nil
}
func (a testSignalingAdapter) MapTransfer(context.Context, interface{}) (CallEvent, error) {
	return CallEvent{}, nil
}
func (a testSignalingAdapter) MapDTMF(context.Context, interface{}) (CallEvent, error) {
	return CallEvent{}, nil
}

type testMediaAdapter struct{ protocol Protocol }

func (a testMediaAdapter) Name() string       { return string(a.protocol) }
func (a testMediaAdapter) Protocol() Protocol { return a.protocol }
func (a testMediaAdapter) MapSessionStarted(context.Context, interface{}) (MediaEvent, error) {
	return MediaEvent{}, nil
}
func (a testMediaAdapter) MapSessionClosed(context.Context, interface{}) (MediaEvent, error) {
	return MediaEvent{}, nil
}
func (a testMediaAdapter) MapTrackMuted(context.Context, interface{}) (MediaEvent, error) {
	return MediaEvent{}, nil
}
func (a testMediaAdapter) MapTrackUnmuted(context.Context, interface{}) (MediaEvent, error) {
	return MediaEvent{}, nil
}
func (a testMediaAdapter) MapRecordingStarted(context.Context, interface{}) (MediaEvent, error) {
	return MediaEvent{}, nil
}
func (a testMediaAdapter) MapRecordingStopped(context.Context, interface{}) (MediaEvent, error) {
	return MediaEvent{}, nil
}

func TestRegistryRegistersProtocols(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterSignaling(testSignalingAdapter{protocol: ProtocolSIP}); err != nil {
		t.Fatalf("register signaling: %v", err)
	}
	if err := registry.RegisterMedia(testMediaAdapter{protocol: ProtocolWebRTC}); err != nil {
		t.Fatalf("register media: %v", err)
	}

	if _, ok := registry.Signaling(ProtocolSIP); !ok {
		t.Fatalf("expected SIP signaling adapter")
	}
	if _, ok := registry.Media(ProtocolWebRTC); !ok {
		t.Fatalf("expected WebRTC media adapter")
	}
	protocols := registry.SupportedProtocols()
	if len(protocols) != 2 {
		t.Fatalf("expected two protocols, got %v", protocols)
	}
}

func TestRegistryRejectsNilAdapter(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterSignaling(nil); err == nil {
		t.Fatalf("expected nil signaling adapter error")
	}
	if err := registry.RegisterMedia(nil); err == nil {
		t.Fatalf("expected nil media adapter error")
	}
}

func TestRegistryNilGuards(t *testing.T) {
	var nilRegistry *Registry
	if err := nilRegistry.RegisterSignaling(testSignalingAdapter{protocol: ProtocolSIP}); err == nil {
		t.Fatal("nil registry RegisterSignaling should fail")
	}
	if err := nilRegistry.RegisterMedia(testMediaAdapter{protocol: ProtocolSIP}); err == nil {
		t.Fatal("nil registry RegisterMedia should fail")
	}
	if _, ok := nilRegistry.Signaling(ProtocolSIP); ok {
		t.Fatal("nil registry Signaling should miss")
	}
	if _, ok := nilRegistry.Media(ProtocolSIP); ok {
		t.Fatal("nil registry Media should miss")
	}
	if got := nilRegistry.SupportedProtocols(); got != nil {
		t.Fatalf("nil registry SupportedProtocols = %v", got)
	}

	registry := NewRegistry()
	if err := registry.RegisterSignaling(nil); err == nil {
		t.Fatal("nil signaling adapter should fail")
	}
	if err := registry.RegisterMedia(nil); err == nil {
		t.Fatal("nil media adapter should fail")
	}
	if _, ok := registry.Signaling(ProtocolSIP); ok {
		t.Fatal("unregistered signaling should miss")
	}
	if _, ok := registry.Media(ProtocolSIP); ok {
		t.Fatal("unregistered media should miss")
	}
	if got := registry.SupportedProtocols(); len(got) != 0 {
		t.Fatalf("empty registry protocols = %v", got)
	}
}

func TestRegistrySignalingOverridesExisting(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterSignaling(testSignalingAdapter{protocol: ProtocolSIP}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.RegisterSignaling(testSignalingAdapter{protocol: ProtocolSIP}); err != nil {
		t.Fatalf("re-register: %v", err)
	}
	adapter, ok := registry.Signaling(ProtocolSIP)
	if !ok || adapter.Protocol() != ProtocolSIP {
		t.Fatalf("Signaling() = (%v, %v)", adapter, ok)
	}
}

func TestRegistrySupportedProtocolsUnionsAndSorts(t *testing.T) {
	registry := NewRegistry()
	if err := registry.RegisterSignaling(testSignalingAdapter{protocol: ProtocolWebRTC}); err != nil {
		t.Fatalf("register signaling: %v", err)
	}
	if err := registry.RegisterMedia(testMediaAdapter{protocol: ProtocolSIP}); err != nil {
		t.Fatalf("register media: %v", err)
	}
	if err := registry.RegisterMedia(testMediaAdapter{protocol: ProtocolWebRTC}); err != nil {
		t.Fatalf("register media ws: %v", err)
	}
	got := registry.SupportedProtocols()
	if len(got) != 2 || got[0] != ProtocolSIP || got[1] != ProtocolWebRTC {
		t.Fatalf("SupportedProtocols() = %v", got)
	}
}
