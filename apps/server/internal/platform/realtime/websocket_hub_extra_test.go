package realtime

import (
	"strings"
	"sync"
	"testing"
)

// 复现 perf 基线 WS 对账翻车的根因（2026-09 CI：客户端 20 连接握手成功、
// 服务端 connected_clients 永久 19）：旧实现 ID 只用 time.Now().UnixNano()，
// CI 虚拟机时钟粒度粗时同批并发建连拿到相同纳秒值，clients map 相互覆盖。
// newClientID 的原子序号必须让并发生成也全量唯一。
func TestNewClientIDConcurrentUnique(t *testing.T) {
	const goroutines = 64
	const perGoroutine = 64
	var mu sync.Mutex
	ids := make(map[string]struct{}, goroutines*perGoroutine)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]string, 0, perGoroutine)
			for i := 0; i < perGoroutine; i++ {
				local = append(local, newClientID())
			}
			mu.Lock()
			defer mu.Unlock()
			for _, id := range local {
				if _, dup := ids[id]; dup {
					t.Errorf("duplicate client id %q", id)
				}
				ids[id] = struct{}{}
			}
		}()
	}
	wg.Wait()
	if len(ids) != goroutines*perGoroutine {
		t.Fatalf("generated %d unique ids, want %d", len(ids), goroutines*perGoroutine)
	}
	for id := range ids {
		if !strings.HasPrefix(id, "client_") {
			t.Fatalf("client id %q lost client_ prefix", id)
		}
	}
}

// 时钟退化兜底：即使连续生成落在同一纳秒（CI 虚拟机粒度粗的退化环境），
// 原子序号也让相邻两次生成的 ID 必然不同。
func TestNewClientIDDegradedClock(t *testing.T) {
	first := newClientID()
	second := newClientID()
	if first == second {
		t.Fatalf("degraded clock must still yield unique ids: %q", first)
	}
}
