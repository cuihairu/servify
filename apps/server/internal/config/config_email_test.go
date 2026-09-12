package config

import (
	"strings"
	"testing"
)

func TestEmailConfigDefaults(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.Email.Enabled {
		t.Fatal("email must default to disabled")
	}
	if cfg.Email.Port != 993 || !cfg.Email.UseTLS {
		t.Fatalf("imap defaults: port=%d use_tls=%v", cfg.Email.Port, cfg.Email.UseTLS)
	}
	if cfg.Email.Mailbox != "INBOX" {
		t.Fatalf("mailbox default = %q", cfg.Email.Mailbox)
	}
	if cfg.Email.PollIntervalSeconds != 60 {
		t.Fatalf("poll interval default = %d", cfg.Email.PollIntervalSeconds)
	}
	if cfg.Email.SMTP.Port != 587 || !cfg.Email.SMTP.UseSTARTTLS {
		t.Fatalf("smtp defaults: port=%d starttls=%v", cfg.Email.SMTP.Port, cfg.Email.SMTP.UseSTARTTLS)
	}
}

func emailWarnings(t *testing.T, mutate func(*Config)) []string {
	t.Helper()
	cfg := GetDefaultConfig()
	mutate(cfg)
	return InsecureDefaults(cfg)
}

func TestInsecureDefaultsEmailDisabledQuiet(t *testing.T) {
	for _, w := range emailWarnings(t, func(*Config) {}) {
		if strings.Contains(w, "email.") {
			t.Fatalf("disabled email must not warn, got %q", w)
		}
	}
}

func TestInsecureDefaultsEmailEnabledRequiresEssentials(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{
			name:   "missing imap host",
			mutate: func(c *Config) { c.Email.Enabled = true },
			want:   "email.host is empty",
		},
		{
			name: "missing credentials",
			mutate: func(c *Config) {
				c.Email.Enabled = true
				c.Email.Host = "imap.example.com"
			},
			want: "email.username/email.password is empty",
		},
		{
			name: "missing smtp host",
			mutate: func(c *Config) {
				c.Email.Enabled = true
				c.Email.Host = "imap.example.com"
				c.Email.Username = "support@example.com"
				c.Email.Password = "secret"
			},
			want: "email.smtp.host is empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			found := false
			for _, w := range emailWarnings(t, tc.mutate) {
				if strings.Contains(w, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected warning containing %q", tc.want)
			}
		})
	}
}

func TestInsecureDefaultsEmailTLSWarnings(t *testing.T) {
	complete := func(c *Config) {
		c.Email.Enabled = true
		c.Email.Host = "imap.example.com"
		c.Email.Username = "support@example.com"
		c.Email.Password = "secret"
		c.Email.SMTP.Host = "smtp.example.com"
	}
	foundPlain := false
	foundSkip := false
	for _, w := range emailWarnings(t, func(c *Config) {
		complete(c)
		c.Email.UseTLS = false
		c.Email.SkipVerify = true
	}) {
		if strings.Contains(w, "email.use_tls is false") {
			foundPlain = true
		}
		if strings.Contains(w, "email.skip_verify is true") {
			foundSkip = true
		}
	}
	if !foundPlain || !foundSkip {
		t.Fatalf("expected TLS warnings, plain=%v skip=%v", foundPlain, foundSkip)
	}
	// 完整安全配置不应再有任何 email 告警
	for _, w := range emailWarnings(t, complete) {
		if strings.Contains(w, "email.") {
			t.Fatalf("secure email config must not warn, got %q", w)
		}
	}
}
