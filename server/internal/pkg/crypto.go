// Package pkg 提供 Server 侧通用工具: 凭证、限速、安全响应头、TLS。
package pkg

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// RandomToken 生成 32 字节随机 Token 的 hex 编码(设计文档 4.2: 持久 Token)。
func RandomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// SHA256Hex 计算 SHA256 并返回 hex(S9: Token/注册码/会话仅存哈希)。
func SHA256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ConstantTimeHexEqual 恒定时间比较两个 hex 串, 避免时序侧信道。
func ConstantTimeHexEqual(a, b string) bool {
	a, b = strings.ToLower(a), strings.ToLower(b)
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// HashPassword bcrypt 哈希管理员密码(cost=12, 设计文档 7.3)。
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword 校验 bcrypt 密码。
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}
