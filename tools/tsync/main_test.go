// 批次 31 跨端守同:cocos / laybox / miniprogram 三端 TS 共享面
// (src/{contract,core,service} 与同名 tests)必须逐字节一致——三端是同构复制,
// 漂移即 bug(批次 30 已手工修过一轮:sed 漏同步 laybox/miniprogram)。
// 平台绑定面(src/platform/**)与端专属 tests(laya/wechat/ui/diagnostics)不参与。
package tsync

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const repoRoot = "../.."

var ends = []string{"cocos", "laybox", "miniprogram"}

var sharedDirs = []string{"src/contract", "src/core", "src/service"}

func listFiles(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repoRoot, dir))
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	out := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s/%s: %v", dir, e.Name(), err)
		}
		out[e.Name()] = data
	}
	return out
}

func TestSharedDirsParity(t *testing.T) {
	var problems []string
	for _, d := range sharedDirs {
		perEnd := map[string]map[string][]byte{}
		for _, e := range ends {
			perEnd[e] = listFiles(t, filepath.Join("sdks", e, d))
		}
		// 文件集合一致:共享目录的文件必须三端同在。
		for _, e := range ends {
			for name := range perEnd[e] {
				for _, o := range ends {
					if o == e {
						continue
					}
					if _, ok := perEnd[o][name]; !ok {
						problems = append(problems, fmt.Sprintf("%s/%s: 仅存在于 %s(缺于 %s)", d, name, e, o))
					}
				}
			}
		}
		// 内容逐字节一致。
		for name := range perEnd[ends[0]] {
			for _, o := range ends[1:] {
				if !bytes.Equal(perEnd[ends[0]][name], perEnd[o][name]) {
					problems = append(problems, fmt.Sprintf("%s/%s: %s 与 %s 内容不一致", d, name, ends[0], o))
				}
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("三端共享面漂移(%d 处):\n%s", len(problems), strings.Join(problems, "\n"))
	}
}

// perEndTests 平台/可选包专属测试:生命周期信号随端实现不同(小游戏 wx.onShow
// vs 小程序 wx.onAppShow;laya/ui/diagnostics 各端自持),不参与同名校验。
var perEndTests = map[string]bool{
	"laya.test.ts":        true,
	"wechat.test.ts":      true,
	"ui.test.ts":          true,
	"diagnostics.test.ts": true,
}

func TestSharedTestsParity(t *testing.T) {
	// 同名 tests 出现于 ≥2 端时必须逐字节一致;端专属测试不参与。
	perEnd := map[string]map[string][]byte{}
	for _, e := range ends {
		perEnd[e] = listFiles(t, filepath.Join("sdks", e, "tests"))
	}
	seen := map[string][]string{}
	for _, e := range ends {
		for name := range perEnd[e] {
			if perEndTests[name] {
				continue
			}
			seen[name] = append(seen[name], e)
		}
	}
	var problems []string
	for name, holders := range seen {
		if len(holders) < 2 {
			continue
		}
		for _, o := range holders[1:] {
			if !bytes.Equal(perEnd[holders[0]][name], perEnd[o][name]) {
				problems = append(problems, fmt.Sprintf("tests/%s: %s 与 %s 内容不一致", name, holders[0], o))
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("同名 tests 漂移(%d 处):\n%s", len(problems), strings.Join(problems, "\n"))
	}
}
