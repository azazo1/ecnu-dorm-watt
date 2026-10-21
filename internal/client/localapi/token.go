package localapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"
)

// TokenPrefix 是本地 token 的固定前缀.
const TokenPrefix = "lw_"

// CookieName 是本地浏览器会话 Cookie 名.
const CookieName = "dormwatt_local"

// GenerateToken 生成 32 字节随机本地 token.
func GenerateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成本地 token 失败: %w", err)
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// ValidateToken 校验本地 token 的前缀与长度.
func ValidateToken(token string) error {
	if !strings.HasPrefix(token, TokenPrefix) {
		return fmt.Errorf("本地 token 必须以 %s 开头", TokenPrefix)
	}
	if len(token) <= len(TokenPrefix) {
		return fmt.Errorf("本地 token 长度不足")
	}
	return nil
}

// TokenEqual 使用常量时间比较两个 token.
func TokenEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// TokenPrefixOf 返回用于展示的 token 前缀, 永远不返回完整 token.
func TokenPrefixOf(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 7 {
		return token
	}
	return token[:7]
}
