package croupier

import (
	"crypto/rand"
	"fmt"
	"time"
)

// newUUIDv7 生成 RFC 9562 UUIDv7(时间有序,契约 primitives.md「ID」)。
// 与 account 包同构实现(跨包不引私有件;一致性由契约测试守护)。
func newUUIDv7(t time.Time) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		for i := range b {
			b[i] = 0
		}
	}
	ms := t.UnixMilli()
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70 // version 7
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
