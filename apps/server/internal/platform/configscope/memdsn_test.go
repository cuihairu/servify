package configscope

import (
	"strconv"
	"sync/atomic"
)

var memSQLiteDBSeq atomic.Uint32

// uniqueMemDSN 给命名内存库 DSN 追加全局唯一序号，
// 避免 -count 重跑、-run 反复执行或并行测试命中同一命名库。
func uniqueMemDSN(base string) string {
	return base + "_" + strconv.FormatUint(uint64(memSQLiteDBSeq.Add(1)), 10) + "?mode=memory&cache=shared"
}
