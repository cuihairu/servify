package recovery

import (
	"errors"
	"time"
)

// 恢复流程的哨兵错误：调用方（cmd/dbrecovery、演练证据）据此区分
// "备份损坏/被篡改"与"目标不可写"等不同失败类别。
var (
	errUnsupportedManifestVersion = errors.New("recovery: unsupported manifest version")
	errUnexpectedArchiveEntry     = errors.New("recovery: archive contains file missing from manifest")
	errMissingArchiveEntry        = errors.New("recovery: manifest file missing from archive")
	errChecksumMismatch           = errors.New("recovery: checksum mismatch")
)

// timeNowUTC 是时间戳 seam：演练证据用确定性时钟断言。
var timeNowUTC = func() string {
	return time.Now().UTC().Format(time.RFC3339)
}
