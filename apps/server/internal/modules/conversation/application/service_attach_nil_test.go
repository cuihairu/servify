package application

import "testing"

// nil 接收者链式安全：与其他 Attach* 一致的守卫分支覆盖。
func TestServiceAttachTransferTargetValidatorNilReceiver(t *testing.T) {
	var s *Service
	if got := s.AttachTransferTargetValidator(&stubTransferValidator{}); got != nil {
		t.Fatal("nil receiver must stay nil")
	}
}
