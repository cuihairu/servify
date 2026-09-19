package application

// 本文件聚合 auth 模块仅供测试注入的包级 seam。每个变量的默认值都保持
// 生产行为，生产代码不得在运行时改写；测试通过替换变量来驱动错误分支
//（crypto/rand 失败、JWT 签名失败）。

import "crypto/rand"

var (
	// hookRandRead 注入 crypto/rand.Read 失败，覆盖 randomHex /
	// newAuthSessionID / newAuthTokenID / generateRecoveryCodes 的降级分支。
	hookRandRead = rand.Read
	// hookCreateHS256JWT 注入 JWT 签名失败，覆盖 access/refresh/challenge
	// token 的错误分支。
	hookCreateHS256JWT = createHS256JWT
)
