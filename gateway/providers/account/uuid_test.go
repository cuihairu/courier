package account

import (
	"strings"
	"testing"
	"time"
)

func TestNewUUIDv7(t *testing.T) {
	now := time.Now()
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := newUUIDv7(now)
		if len(id) != 36 || strings.Count(id, "-") != 4 {
			t.Fatalf("uuid 形状非法: %q", id)
		}
		if seen[id] {
			t.Fatalf("uuid 重复: %q", id)
		}
		seen[id] = true
	}
	// version 与 variant 位
	id := newUUIDv7(now)
	if id[14] != '7' {
		t.Fatalf("version nibble = %q, 期望 7", id[14])
	}
	if id[19] != '8' && id[19] != '9' && id[19] != 'a' && id[19] != 'b' {
		t.Fatalf("variant nibble = %q, 期望 8/9/a/b", id[19])
	}
	// 时间有序(毫秒前缀随时间推进)
	later := newUUIDv7(now.Add(2 * time.Millisecond))
	if later <= id {
		t.Fatalf("uuidv7 应时间有序: %q !> %q", later, id)
	}
}
