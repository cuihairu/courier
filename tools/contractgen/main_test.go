package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// 测试运行目录 = 本包目录(tools/contractgen),仓库根在其上两级。
const repoRoot = "../.."

func readRepoFile(t *testing.T, rel string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("读 %s: %v", rel, err)
	}
	return raw
}

func loadErrorsFixture(t *testing.T) errorsFixture {
	t.Helper()
	var f errorsFixture
	raw := readRepoFile(t, "docs/contract/fixtures/errors.json")
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("解析 errors.json: %v", err)
	}
	return f
}

// TestErrorsMarkdownMatchesFixture 冻结文档 ↔ fixture 不漂移:错误码表与域前缀
// 注册表逐行对齐(顺序、HTTP、retryable)。
func TestErrorsMarkdownMatchesFixture(t *testing.T) {
	md := string(readRepoFile(t, "docs/contract/errors.md"))
	f := loadErrorsFixture(t)

	rowRe := regexp.MustCompile(`(?m)^\| ([A-Z][A-Z0-9_]*) \| (\d{3}) \| (true|false) \|`)
	matches := rowRe.FindAllStringSubmatch(md, -1)
	if len(matches) != len(f.Codes) {
		t.Fatalf("errors.md 错误码表有 %d 行,fixture 有 %d 码", len(matches), len(f.Codes))
	}
	for i, m := range matches {
		want := f.Codes[i]
		if m[1] != want.Code || m[2] != itoa(want.HTTP) || m[3] != lower(want.Retryable) {
			t.Errorf("第 %d 行不一致:md=(%s,%s,%s) fixture=(%s,%d,%v)",
				i+1, m[1], m[2], m[3], want.Code, want.HTTP, want.Retryable)
		}
	}

	// 域前缀注册表:反引号内大写词序列。
	prefixRe := regexp.MustCompile("`([A-Z]+)`")
	var mdPrefixes []string
	for _, line := range regexp.MustCompile(`(?m)^.*$`).FindAllString(md, -1) {
		if !strings_contains(line, "`COMMON` /") {
			continue
		}
		for _, p := range prefixRe.FindAllStringSubmatch(line, -1) {
			mdPrefixes = append(mdPrefixes, p[1])
		}
	}
	if !reflect.DeepEqual(mdPrefixes, f.Prefixes) {
		t.Errorf("域前缀不一致:md=%v fixture=%v", mdPrefixes, f.Prefixes)
	}
}

// TestScopeAndDtoFixtures 完整性。
func TestScopeAndDtoFixtures(t *testing.T) {
	var sf scopeFixture
	if err := json.Unmarshal(readRepoFile(t, "docs/contract/fixtures/scope.json"), &sf); err != nil {
		t.Fatalf("解析 scope.json: %v", err)
	}
	if sf.Headers["gameId"] == "" || sf.Headers["env"] == "" {
		t.Fatalf("scope.json headers 不完整: %+v", sf.Headers)
	}
	if len(sf.Envs) < 3 {
		t.Fatalf("scope.json envs 至少含 dev/staging/prod: %v", sf.Envs)
	}

	var df dtoFixture
	if err := json.Unmarshal(readRepoFile(t, "docs/contract/fixtures/dto.json"), &df); err != nil {
		t.Fatalf("解析 dto.json: %v", err)
	}
	for _, k := range []string{"data", "error", "traceId", "code", "message", "retryable", "items", "nextCursor"} {
		if df.Keys[k] == "" {
			t.Errorf("dto.json 缺 key %q", k)
		}
	}
}

// TestGeneratedGdNoSlashComments GDScript 无 // 注释——Go/C 风格会被引擎判为语法错误
// (SCRIPT ERROR: Unexpected "/" in class body),生成物行首注释必须是 #。
func TestGeneratedGdNoSlashComments(t *testing.T) {
	for rel, want := range generate(repoRoot) {
		if !strings.HasSuffix(rel, ".gd") {
			continue
		}
		for i, line := range strings.Split(string(want), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				t.Errorf("%s 第 %d 行是 // 注释(GDScript 不认,须 #): %s", rel, i+1, line)
			}
		}
	}
}

// TestGeneratedFilesInSync 生成物与仓内文件逐字节一致(改 fixture 后需 -write 重新生成)。
func TestGeneratedFilesInSync(t *testing.T) {
	files := generate(repoRoot)
	if len(files) == 0 {
		t.Fatal("generate 未产出任何文件")
	}
	for rel, want := range files {
		have, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Errorf("缺失 %s:%v(运行 go run ./tools/contractgen -write)", rel, err)
			continue
		}
		if string(have) != string(want) {
			t.Errorf("漂移 %s(与 fixture 不一致;运行 go run ./tools/contractgen -write)", rel)
		}
	}
}

// TestGenerateDeterministic 两次生成逐字节一致(防 map 遍历序泄漏进产物)。
func TestGenerateDeterministic(t *testing.T) {
	a := generate(repoRoot)
	b := generate(repoRoot)
	if len(a) != len(b) {
		t.Fatalf("生成文件数不稳定: %d vs %d", len(a), len(b))
	}
	for k, va := range a {
		if vb, ok := b[k]; !ok || string(va) != string(vb) {
			t.Errorf("生成不确定: %s", k)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func strings_contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
