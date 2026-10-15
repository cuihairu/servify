// Package quotameter 提供固定窗口配额计量的 Redis 实现（租户级配额限流
// enforcement，docs/design/tenant-quota-policy.md §3 计量存储选型决议）：
// 窗口锚点编进键名，窗口切换自然归零重计；TTL 兜底自清理旧键。不建 DB
// 用量表——窗口状态天然瞬态，redis 重启归零的后果只是多放行一窗请求
// （429 保护面，非计费面），不产生资损。
package quotameter

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// WindowKind 固定窗口档位（枚举与 config translation.quota.window 对齐）。
type WindowKind string

const (
	WindowHour  WindowKind = "hour"
	WindowDay   WindowKind = "day"
	WindowMonth WindowKind = "month"
)

// ParseWindow 解析窗口档位字串（装配期 fail-fast 校验入口）。
func ParseWindow(s string) (WindowKind, error) {
	switch k := WindowKind(s); k {
	case WindowHour, WindowDay, WindowMonth:
		return k, nil
	default:
		return "", fmt.Errorf("invalid quota window %q (want hour|day|month)", s)
	}
}

// graceTTL 窗口长度之上的兜底宽限：锚点切走的旧键靠 TTL 自清理。
const graceTTL = 10 * time.Minute

// Meter 固定窗口计量器：Usage 读当前窗口累计值，Add 原子累加并返回
// 累加后的窗口总量。键形如 servify:quota:{namespace}:{window}:{anchor}:{scope}。
type Meter struct {
	client    *redis.Client
	namespace string
	window    WindowKind
	now       func() time.Time
}

// New 创建计量器；window 非法或 client 为 nil 即报错（装配期校验）。
func New(client *redis.Client, namespace string, window WindowKind) (*Meter, error) {
	if _, err := ParseWindow(string(window)); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("quotameter: redis client is nil")
	}
	return &Meter{client: client, namespace: namespace, window: window, now: time.Now}, nil
}

// anchor 窗口锚点：hour→整点、day→当日、month→当月（本地时区口径，
// 与进程部署时区一致即可；多实例共享同一 redis 键自然对齐）。
func (m *Meter) anchor(now time.Time) string {
	switch m.window {
	case WindowHour:
		return now.Format("20060102T15")
	case WindowDay:
		return now.Format("20060102")
	default:
		return now.Format("200601")
	}
}

// key 拼装当前锚点下的计量键。
func (m *Meter) key(scope string, now time.Time) string {
	return fmt.Sprintf("servify:quota:%s:%s:%s:%s", m.namespace, m.window, m.anchor(now), scope)
}

// ttl 当前窗口剩余长度 + 宽限。
func (m *Meter) ttl(now time.Time) time.Duration {
	switch m.window {
	case WindowHour:
		end := now.Truncate(time.Hour).Add(time.Hour)
		return end.Sub(now) + graceTTL
	case WindowDay:
		end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1)
		return end.Sub(now) + graceTTL
	default:
		end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, 1, 0)
		return end.Sub(now) + graceTTL
	}
}

// Usage 读取 scope 当前窗口累计值；键不存在视为 0（窗口尚未开始计量）。
func (m *Meter) Usage(ctx context.Context, scope string) (int64, error) {
	val, err := m.client.Get(ctx, m.key(scope, m.now())).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("quotameter: malformed counter %q: %w", val, err)
	}
	return n, nil
}

// Add 原子累加 amount 并返回累加后的窗口总量；同时给键兜底设置 TTL
// （仅无 TTL 时设置，ExpireNX），redis 丢键后从当前值重新计。
func (m *Meter) Add(ctx context.Context, scope string, amount int64) (int64, error) {
	now := m.now()
	key := m.key(scope, now)
	pipe := m.client.Pipeline()
	incr := pipe.IncrBy(ctx, key, amount)
	pipe.ExpireNX(ctx, key, m.ttl(now))
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}
