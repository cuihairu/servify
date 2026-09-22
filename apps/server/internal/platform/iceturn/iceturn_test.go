package iceturn

// iceturn_test.go 覆盖时间限凭据生成与 ICE 装配的全部分支；
// golden 向量经独立实现（python hmac）预计算，防止实现自证。

import (
	"encoding/base64"
	"testing"
	"time"
)

var fixedNow = time.Unix(1790000000, 0) // 2026-09 附近固定时刻，凭据 = 1790000300

func TestRESTCredentialGoldenVector(t *testing.T) {
	got := RESTCredential("test-secret", 5*time.Minute, fixedNow)
	if got.Username != "1790000300" {
		t.Fatalf("Username = %q, want %q", got.Username, "1790000300")
	}
	if got.Credential != "isReWBKNlmmMSS3VR4Xr9PtPqYE=" {
		t.Fatalf("Credential = %q, want golden HMAC-SHA1 base64", got.Credential)
	}
}

func TestRESTCredentialEmptySecretIsStillDeterministic(t *testing.T) {
	got := RESTCredential("", DefaultTTL, fixedNow)
	if got.Credential != "AHK0WQR+K8kT6THh8pxzbOzoohE=" {
		t.Fatalf("Credential = %q, want golden empty-secret vector", got.Credential)
	}
}

func TestRESTCredentialNonPositiveTTLFallsBackToDefault(t *testing.T) {
	fallback := RESTCredential("test-secret", 0, fixedNow)
	if fallback.Username != "1790000300" {
		t.Fatalf("fallback TTL username = %q, want DefaultTTL-based %q", fallback.Username, "1790000300")
	}
	plain := RESTCredential("test-secret", DefaultTTL, fixedNow)
	if fallback != plain {
		t.Fatalf("fallback credential %+v differs from explicit DefaultTTL %+v", fallback, plain)
	}
}

func TestRESTCredentialExpiresWithTTL(t *testing.T) {
	long := RESTCredential("test-secret", time.Hour, fixedNow)
	if long.Username != "1790003600" {
		t.Fatalf("hour TTL username = %q, want %q", long.Username, "1790003600")
	}
	decoded, err := base64.StdEncoding.DecodeString(long.Credential)
	if err != nil {
		t.Fatalf("credential is not valid base64: %v", err)
	}
	if len(decoded) != 20 {
		t.Fatalf("decoded credential length = %d, want SHA-1 digest size 20", len(decoded))
	}
}

func TestConfigEnabled(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"empty disabled", "", false},
		{"blank disabled", "   ", false},
		{"url enabled", "turn:turn.example.com:3478", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := (Config{URL: tc.url}).Enabled(); got != tc.want {
				t.Fatalf("Enabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConfigValidateDisabledIsAlwaysValid(t *testing.T) {
	// 未启用时其余字段残缺也不拦（禁用语义优先于完整性）。
	if err := (Config{}).Validate(); err != nil {
		t.Fatalf("Validate() on disabled config = %v, want nil", err)
	}
	if err := (Config{URL: " ", Realm: "", StaticAuthSecret: "x"}).Validate(); err != nil {
		t.Fatalf("Validate() on blank-url config = %v, want nil", err)
	}
}

func TestConfigValidateEnabledRequiresCompleteConfig(t *testing.T) {
	complete := Config{
		URL:              "turn:turn.example.com:3478",
		Realm:            "servify.example.com",
		StaticAuthSecret: "shared-secret",
		TTL:              5 * time.Minute,
	}
	if err := complete.Validate(); err != nil {
		t.Fatalf("Validate() on complete config = %v, want nil", err)
	}

	cases := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"missing realm", func(c *Config) { c.Realm = " " }, "webrtc.turn.url is set but webrtc.turn.realm is empty"},
		{"missing secret", func(c *Config) { c.StaticAuthSecret = "" }, "webrtc.turn.url is set but webrtc.turn.static_auth_secret is empty"},
		{"non-positive ttl", func(c *Config) { c.TTL = 0 }, "webrtc.turn.url is set but webrtc.turn.ttl is not positive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			broken := complete
			tc.mutate(&broken)
			err := broken.Validate()
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Validate() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestConfigWithDefaults(t *testing.T) {
	zero := Config{URL: "", TTL: 0}
	if got := zero.WithDefaults(); got.TTL != DefaultTTL {
		t.Fatalf("WithDefaults() on zero TTL = %v, want %v", got.TTL, DefaultTTL)
	}
	enabled := Config{URL: "turn:turn.example.com:3478", TTL: 0}
	if got := enabled.WithDefaults(); got.TTL != DefaultTTL {
		t.Fatalf("WithDefaults() TTL = %v, want %v", got.TTL, DefaultTTL)
	}
	kept := Config{URL: "turn:turn.example.com:3478", TTL: time.Hour}
	if got := kept.WithDefaults(); got.TTL != time.Hour {
		t.Fatalf("WithDefaults() overrode explicit TTL: %v", got.TTL)
	}
}

func TestAssembleTurnDisabled(t *testing.T) {
	got := Assemble([]string{"stun:stun.example.com:3478"}, "stun:legacy:3478", Config{URL: ""}, fixedNow)
	if len(got.STUNServers) != 1 || got.STUNServers[0] != "stun:stun.example.com:3478" {
		t.Fatalf("STUNServers = %v, want single entry", got.STUNServers)
	}
	if got.TURNURL != "" || got.TURNUsername != "" || got.TURNCredential != "" {
		t.Fatalf("disabled TURN leaked credentials: %+v", got)
	}
}

func TestAssembleStunListCleanupAndPrecedence(t *testing.T) {
	got := Assemble(
		[]string{"  stun:a:3478 ", "", "stun:a:3478", "stun:b:3478"},
		"stun:legacy:3478",
		Config{},
		fixedNow,
	)
	want := []string{"stun:a:3478", "stun:b:3478"}
	if len(got.STUNServers) != len(want) {
		t.Fatalf("STUNServers = %v, want trimmed/deduped %v", got.STUNServers, want)
	}
	for i := range want {
		if got.STUNServers[i] != want[i] {
			t.Fatalf("STUNServers[%d] = %q, want %q", i, got.STUNServers[i], want[i])
		}
	}
}

func TestAssembleLegacySTUNFallback(t *testing.T) {
	got := Assemble(nil, "stun:stun.l.google.com:19302", Config{}, fixedNow)
	if len(got.STUNServers) != 1 || got.STUNServers[0] != "stun:stun.l.google.com:19302" {
		t.Fatalf("STUNServers = %v, want legacy fallback", got.STUNServers)
	}

	empty := Assemble(nil, "  ", Config{}, fixedNow)
	if len(empty.STUNServers) != 0 {
		t.Fatalf("blank legacy STUN should stay empty, got %v", empty.STUNServers)
	}
}

func TestAssembleTurnEnabledIssuesShortLivedCredential(t *testing.T) {
	turn := Config{
		URL:              " turn:turn.example.com:3478 ",
		Realm:            "servify.example.com",
		StaticAuthSecret: "test-secret",
		TTL:              5 * time.Minute,
	}
	got := Assemble([]string{"stun:stun.example.com:3478"}, "stun:legacy:3478", turn, fixedNow)
	if got.TURNURL != "turn:turn.example.com:3478" {
		t.Fatalf("TURNURL = %q, want trimmed", got.TURNURL)
	}
	if got.TURNUsername != "1790000300" {
		t.Fatalf("TURNUsername = %q, want expiry-based username", got.TURNUsername)
	}
	if got.TURNCredential != "isReWBKNlmmMSS3VR4Xr9PtPqYE=" {
		t.Fatalf("TURNCredential = %q, want golden vector", got.TURNCredential)
	}
	if got.TURNTTL != 5*time.Minute {
		t.Fatalf("TURNTTL = %v, want configured 5m", got.TURNTTL)
	}
}

func TestAssembleTurnCredentialsRotatedByNow(t *testing.T) {
	turn := Config{URL: "turn:t:3478", Realm: "r", StaticAuthSecret: "s", TTL: time.Minute}
	earlier := Assemble(nil, "", turn, fixedNow)
	later := Assemble(nil, "", turn, fixedNow.Add(time.Minute))
	if earlier.TURNUsername == later.TURNUsername {
		t.Fatalf("username should track expiry, both %q", earlier.TURNUsername)
	}
	if earlier.TURNCredential == later.TURNCredential {
		t.Fatalf("credential should rotate with username")
	}
}

func TestAssembleTurnZeroTTLUsesDefault(t *testing.T) {
	turn := Config{URL: "turn:t:3478", Realm: "r", StaticAuthSecret: "s", TTL: 0}
	got := Assemble(nil, "", turn, fixedNow)
	if got.TURNTTL != DefaultTTL {
		t.Fatalf("TURNTTL = %v, want DefaultTTL %v", got.TURNTTL, DefaultTTL)
	}
	if got.TURNUsername != "1790000300" {
		t.Fatalf("TURNUsername = %q, want DefaultTTL-based expiry", got.TURNUsername)
	}
}

func TestICEDescribe(t *testing.T) {
	disabled := Assemble([]string{"stun:a:3478", "stun:b:3478"}, "", Config{}, fixedNow)
	if got, want := disabled.Describe(), "stun=2,turn=disabled"; got != want {
		t.Fatalf("Describe() = %q, want %q", got, want)
	}
	enabled := Assemble(nil, "", Config{URL: "turn:t:3478", Realm: "r", StaticAuthSecret: "s", TTL: time.Minute}, fixedNow)
	if got, want := enabled.Describe(), "stun=0,turn=turn:t:3478,realm-credential=issued"; got != want {
		t.Fatalf("Describe() = %q, want %q", got, want)
	}
}
