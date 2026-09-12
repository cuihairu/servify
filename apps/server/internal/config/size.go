package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseSizeBytes 将形如 "32MB"、"512KB"、"1048576" 的容量配置解析为字节数。
// 空串返回 0；纯数字按字节；支持 KB/MB/GB（1024 进制）。
func ParseSizeBytes(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, nil
	}
	// 纯数字直接解析为字节
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	// 含单位
	units := []struct {
		suffix string
		mul    int64
	}{
		{"KB", 1024},
		{"MB", 1024 * 1024},
		{"GB", 1024 * 1024 * 1024},
	}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			val := strings.TrimSuffix(s, u.suffix)
			n, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
			if err != nil {
				return 0, err
			}
			return int64(n * float64(u.mul)), nil
		}
	}
	return 0, fmt.Errorf("unknown size format: %s", s)
}
