package application

// 本文件聚合 auth 模块仅供测试注入的包级 seam。每个变量的默认值都保持
// 生产行为，生产代码不得在运行时改写；测试（含 infra 层的 db 型测试）通过
// 替换变量来驱动错误分支（crypto/rand 失败、JWT 签名失败）。seam 必须导出，
// 因为 gorm 仓储测试住在 infra 包，无法触达应用层的包级私有变量。

import "crypto/rand"

var (
	// HookRandRead 注入 crypto/rand.Read 失败，覆盖 randomHex /
	// newAuthSessionID / newAuthTokenID / GenerateRecoveryCodes 的降级分支。
	HookRandRead = rand.Read
	// HookCreateHS256JWT 注入 JWT 签名失败，覆盖 access/refresh/challenge
	// token 的错误分支。
	HookCreateHS256JWT = createHS256JWT
)
