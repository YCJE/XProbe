package api

import (
	"crypto/sha256"
	"encoding/hex"
)

// fpHash 生成测试用 64 位十六进制"指纹"。
func fpHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
