package config

import (
	"crypto/rand"
	"encoding/hex"
)

// randomToken 生成 n 字节的随机 hex 字符串（长度 2n）。
func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand 失败属于环境级故障，直接崩
	}
	return hex.EncodeToString(b)
}
