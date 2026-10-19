package main

import (
	"encoding/json"
	"io"
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
	// 事实源是中文契约(i18n 迁移后位于 docs/zh/contract/,root 为英文镜像)。
	md := string(readRepoFile(t, "docs/zh/contract/errors.md"))
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

// TestErrorCodesWiredOrReserved 错误码接线闭环:errors.json 的每个 code 要么在网关
// 非测试代码里被引用(已接线),要么在 errors.md「注册保留码」注记里被点名(显式保留)。
// 两边集合必须相等——新增码忘接线、或接线后忘从注记里摘掉,都在这里失败。
// 判据说明:只认标识符在聚合层常量表(envelope.go)之外的引用,故 codeTable 里的
// HTTP 映射不算接线;测试代码不算接线(断言不是运行时行为)。
func TestErrorCodesWiredOrReserved(t *testing.T) {
	f := loadErrorsFixture(t)

	// wire code → 聚合层常量名(Code<Name> = "WIRE")。
	constRe := regexp.MustCompile(`(?m)^\s*(Code[A-Za-z0-9]+)\s*=\s*"([A-Z][A-Z0-9_]*)"`)
	constOf := map[string]string{}
	for _, m := range constRe.FindAllStringSubmatch(string(readRepoFile(t, "gateway/aggregation/envelope.go")), -1) {
		constOf[m[2]] = m[1]
	}

	// 网关非测试代码里出现过的常量标识符集合。
	identRe := regexp.MustCompile(`\bCode[A-Za-z0-9]+\b`)
	referenced := map[string]bool{}
	root := filepath.Join(repoRoot, "gateway")
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.Base(path) == "envelope.go" {
			return nil // 常量定义与 HTTP 映射表本身,不算引用
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, id := range identRe.FindAllString(string(raw), -1) {
			referenced[id] = true
		}
		return nil
	}); err != nil {
		t.Fatalf("遍历 gateway: %v", err)
	}

	// errors.md「注册保留码」注记点名的码。
	reservedRe := regexp.MustCompile("`([A-Z][A-Z0-9_]*)`")
	reserved := map[string]bool{}
	for _, line := range strings.Split(string(readRepoFile(t, "docs/zh/contract/errors.md")), "\n") {
		if !strings.Contains(line, "注册保留码") {
			continue
		}
		for _, m := range reservedRe.FindAllStringSubmatch(line, -1) {
			reserved[m[1]] = true
		}
	}
	if len(reserved) == 0 {
		t.Fatal("errors.md 未找到「注册保留码」注记")
	}

	for _, c := range f.Codes {
		name, ok := constOf[c.Code]
		if !ok {
			t.Errorf("%s 在 errors.json 有,聚合层无对应常量", c.Code)
			continue
		}
		wired := referenced[name]
		switch {
		case wired && reserved[c.Code]:
			t.Errorf("%s 已接线,应从「注册保留码」注记里摘掉", c.Code)
		case !wired && !reserved[c.Code]:
			t.Errorf("%s 网关无引用且未在「注册保留码」注记里声明(悄悄保留 = 漂移)", c.Code)
		}
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

// TestCheckOrWriteRoundTrip 生成→落盘→复核全链:临时根上装 fixtures 后
// -write 生成,再复查应一致(覆盖 checkOrWrite 写与验两分支;漂移分支经
// 记录型 exit 断言,见 TestCheckOrWriteDriftFatal)。
func TestCheckOrWriteRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	stageFixtures(t, tmp)
	checkOrWrite(tmp, true)  // 生成落盘(写分支)
	checkOrWrite(tmp, false) // 复核一致(验分支)
}

// stageFixtures 把仓内契约 fixtures 复制到目标根(临时根驱动生成链的前置)。
func stageFixtures(t *testing.T, root string) {
	t.Helper()
	for _, f := range []string{"errors.json", "scope.json", "dto.json"} {
		rel := filepath.Join("docs", "contract", "fixtures", f)
		dst := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, readRepoFile(t, rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMainWriteMode 覆盖 main():flag 解析 + 写分支(临时根装 fixtures 后 -write)。
// 不跑仓根校验模式,避免把仓内漂移变成测试进程 os.Exit(漂移由 TestGeneratedFilesInSync 暴露)。
func TestMainWriteMode(t *testing.T) {
	tmp := t.TempDir()
	stageFixtures(t, tmp)
	oldArgs := os.Args
	os.Args = []string{"contractgen", "-root", tmp, "-write"}
	defer func() { os.Args = oldArgs }()

	main() // 入口全链:flag.Parse → checkOrWrite(write)

	files := generate(tmp)
	if len(files) == 0 {
		t.Fatal("generate 未产出任何文件")
	}
	for rel := range files {
		if _, err := os.Stat(filepath.Join(tmp, rel)); err != nil {
			t.Errorf("-write 后缺 %s: %v", rel, err)
		}
	}
}

// captureIO 把 exit 换成记录型、stdout/stderr 换成内存管道,执行 action 后
// 返回记录的退出码与两路输出(fatal 不真退进程;生产 exit = os.Exit 行为不变)。
func captureIO(t *testing.T, action func()) (code int, stdout, stderr string) {
	t.Helper()
	oldExit, oldOut, oldErr := exit, os.Stdout, os.Stderr
	code = 0
	exit = func(c int) { code = c } // fatal 尾部调用后返回,不真退
	or, ow, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	er, ew, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = ow, ew
	defer func() {
		os.Stdout, os.Stderr = oldOut, oldErr
		exit = oldExit
	}()

	action()
	ow.Close()
	ew.Close()
	o, err := io.ReadAll(or)
	if err != nil {
		t.Fatal(err)
	}
	e, err := io.ReadAll(er)
	if err != nil {
		t.Fatal(err)
	}
	return code, string(o), string(e)
}

// TestGenerateIncompleteFixtureFatal fixtures 不完整(版本<1/码表空前缀空、dto 缺
// key):致命退出 1 且信息点名缺哪项——fixture 契约的强制分支。
func TestGenerateIncompleteFixtureFatal(t *testing.T) {
	t.Run("errors 不完整", func(t *testing.T) {
		tmp := t.TempDir()
		stageFixtures(t, tmp)
		bad := filepath.Join(tmp, "docs", "contract", "fixtures", "errors.json")
		if err := os.WriteFile(bad, []byte(`{"version":0,"prefixes":["COMMON"],"codes":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		code, _, stderr := captureIO(t, func() { generate(tmp) })
		if code != 1 {
			t.Errorf("不完整 errors fixture 应致命退出 1,实得 %d", code)
		}
		if !strings.Contains(stderr, "errors fixture 不完整") {
			t.Errorf("stderr 缺不完整信息: %q", stderr)
		}
	})
	t.Run("dto 缺 key", func(t *testing.T) {
		tmp := t.TempDir()
		stageFixtures(t, tmp)
		bad := filepath.Join(tmp, "docs", "contract", "fixtures", "dto.json")
		if err := os.WriteFile(bad, []byte(`{"version":1,"keys":{}}`), 0o644); err != nil {
			t.Fatal(err)
		}
		code, _, stderr := captureIO(t, func() { generate(tmp) })
		if code != 1 {
			t.Errorf("缺 key dto fixture 应致命退出 1,实得 %d", code)
		}
		if !strings.Contains(stderr, `dto fixture 缺 key "data"`) {
			t.Errorf("stderr 缺缺 key 信息: %q", stderr)
		}
	})
}

// TestFatalExitsNonZero fatal:stderr 带 contractgen: 前缀写致命信息,退出码 1。
func TestFatalExitsNonZero(t *testing.T) {
	code, _, stderr := captureIO(t, func() { fatal("测试触发的致命错误 %d", 42) })
	if code != 1 {
		t.Errorf("fatal 退出码应为 1,实得 %d", code)
	}
	if !strings.Contains(stderr, "contractgen: 测试触发的致命错误 42") {
		t.Errorf("stderr 缺致命信息: %q", stderr)
	}
}

// TestReadJSONFatalBranches fixture 缺失与 JSON 损坏:各自致命退出码 1,信息可辨。
func TestReadJSONFatalBranches(t *testing.T) {
	var sink any
	t.Run("文件缺失", func(t *testing.T) {
		code, _, stderr := captureIO(t, func() {
			readJSON(filepath.Join(t.TempDir(), "无此.json"), &sink)
		})
		if code != 1 {
			t.Errorf("缺失 fixture 应致命退出 1,实得 %d", code)
		}
		if !strings.Contains(stderr, "读 fixture") {
			t.Errorf("stderr 缺读取错误信息: %q", stderr)
		}
	})
	t.Run("JSON 损坏", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "坏.json")
		if err := os.WriteFile(bad, []byte("{坏"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, _, stderr := captureIO(t, func() { readJSON(bad, &sink) })
		if code != 1 {
			t.Errorf("损坏 JSON 应致命退出 1,实得 %d", code)
		}
		if !strings.Contains(stderr, "解析 "+bad) {
			t.Errorf("stderr 缺解析错误信息: %q", stderr)
		}
	})
}

// TestCheckOrWriteDriftFatal 缺文件与内容漂移:同一轮各命中一支,汇总致命信息
// 指回 -write(校验模式下重生成;stdout 逐文件行缺失/漂移)。
func TestCheckOrWriteDriftFatal(t *testing.T) {
	tmp := t.TempDir()
	stageFixtures(t, tmp)
	checkOrWrite(tmp, true) // 落盘生成物

	files := generate(tmp)
	rels := make([]string, 0, 2)
	for rel := range files {
		rels = append(rels, rel)
		if len(rels) == 2 {
			break
		}
	}
	if err := os.Remove(filepath.Join(tmp, rels[0])); err != nil {
		t.Fatal(err)
	}
	corrupted := append([]byte("x"), files[rels[1]]...)
	if err := os.WriteFile(filepath.Join(tmp, rels[1]), corrupted, 0o644); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := captureIO(t, func() { checkOrWrite(tmp, false) })
	if code != 1 {
		t.Errorf("漂移应致命退出 1,实得 %d", code)
	}
	if !strings.Contains(stdout, "缺失: "+rels[0]) {
		t.Errorf("stdout 缺缺失行: %q", stdout)
	}
	if !strings.Contains(stdout, "漂移: "+rels[1]) {
		t.Errorf("stdout 缺漂移行: %q", stdout)
	}
	if !strings.Contains(stderr, "生成物与仓内文件不一致") {
		t.Errorf("stderr 缺汇总致命信息: %q", stderr)
	}
}
