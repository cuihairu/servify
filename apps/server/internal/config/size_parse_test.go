package config

import "testing"

func TestParseSizeBytes(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"empty", "", 0, false},
		{"blank", "   ", 0, false},
		{"plain bytes", "1048576", 1048576, false},
		{"plain bytes padded", " 4096 ", 4096, false},
		{"kb uppercase", "32KB", 32 * 1024, false},
		{"kb lowercase", "32kb", 32 * 1024, false},
		{"mb", "512MB", 512 * 1024 * 1024, false},
		{"gb", "2GB", 2 * 1024 * 1024 * 1024, false},
		{"fractional unit", "1.5MB", int64(1.5 * 1024 * 1024), false},
		{"unit padded", "  8 MB ", 8 * 1024 * 1024, false},
		{"zero with unit", "0KB", 0, false},
		{"negative bytes", "-7", -7, false},
		{"unknown unit", "12XB", 0, true},
		{"garbage", "abc", 0, true},
		{"broken float", "1.2.3KB", 0, true},
		{"negative unit", "-3MB", int64(-3 * 1024 * 1024), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseSizeBytes(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseSizeBytes(%q) = %d, want error", tc.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseSizeBytes(%q) error = %v", tc.input, err)
			}
			if got != tc.want {
				t.Fatalf("ParseSizeBytes(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}
