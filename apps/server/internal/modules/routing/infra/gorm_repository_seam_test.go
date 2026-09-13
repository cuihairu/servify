package infra

import (
	"errors"
	"testing"
)

func TestMarshalSkillsJSONFailureReturnsEmpty(t *testing.T) {
	orig := marshalSkillsJSON
	marshalSkillsJSON = func(v interface{}) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { marshalSkillsJSON = orig }()

	if got := marshalSkills([]string{"go", "redis"}); got != "" {
		t.Fatalf("expected empty string on marshal failure, got %q", got)
	}
}

func TestMarshalSkillsEncodesList(t *testing.T) {
	// seam 默认路径：真实 json.Marshal 输出 JSON 数组。
	if got := marshalSkills([]string{"go", "redis"}); got != `["go","redis"]` {
		t.Fatalf("unexpected encoding: %q", got)
	}
	if got := marshalSkills(nil); got != "" {
		t.Fatalf("expected empty string for nil skills, got %q", got)
	}
}
