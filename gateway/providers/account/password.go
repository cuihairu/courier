package account

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
)

// AlgoPBKDF2SHA256 凭证算法标识(契约 auth.md:account_credentials.algo)。
const AlgoPBKDF2SHA256 = "PBKDF2-SHA256"

// DefaultIterations PBKDF2 迭代数默认值(测试经 Options 调低)。
const DefaultIterations = 210000

const saltLen = 16
const hashLen = 32

// hashPassword 派生 PBKDF2-HMAC-SHA256(salt 随机 16B);契约:禁止明文、禁止可逆。
func hashPassword(password string, iterations int) (hash, salt []byte, err error) {
	salt = make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, fmt.Errorf("account: 生成 salt: %w", err)
	}
	dk, err := pbkdf2.Key(sha256.New, password, salt, iterations, hashLen)
	if err != nil {
		return nil, nil, fmt.Errorf("account: 派生密码哈希: %w", err)
	}
	return dk, salt, nil
}

// verifyPassword 常数时间比对。
func verifyPassword(password string, salt, want []byte, iterations int) bool {
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, hashLen)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}
