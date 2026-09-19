package services

// 测试辅助函数

import "time"

func stringPtr(s string) *string {
	return &s
}

func uintPtr(i uint) *uint {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}

func intPtr(i int) *int {
	return &i
}

func timePtr(t time.Time) *time.Time { return &t }
