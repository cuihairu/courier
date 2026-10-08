package account

import (
	"bytes"
	"testing"
)

func TestHashPassword(t *testing.T) {
	h1, s1, err := hashPassword("s3cret-password", 1000)
	if err != nil {
		t.Fatalf("hashPassword 失败: %v", err)
	}
	if bytes.Equal(h1, []byte("s3cret-password")) {
		t.Fatal("哈希不得等于明文")
	}
	if len(s1) != saltLen || len(h1) != hashLen {
		t.Fatalf("salt/hash 长度 = %d/%d, 期望 %d/%d", len(s1), len(h1), saltLen, hashLen)
	}

	// 同密码两次哈希 salt 不同(防彩虹表)
	h2, s2, _ := hashPassword("s3cret-password", 1000)
	if bytes.Equal(s1, s2) {
		t.Fatal("两次哈希 salt 不应相同")
	}
	if bytes.Equal(h1, h2) {
		t.Fatal("不同 salt 应产生不同哈希")
	}

	if !verifyPassword("s3cret-password", s1, h1, 1000) {
		t.Fatal("正确密码应通过验证")
	}
	if verifyPassword("wrong-password", s1, h1, 1000) {
		t.Fatal("错误密码不应通过验证")
	}
}
