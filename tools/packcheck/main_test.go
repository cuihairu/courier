// 批次 5 结构性验收:Unity 三包(UPM)骨架与 L3 Adapter 落位检查。
// UnityEngine 代码 dotnet 编译不到——用结构断言守住:包元数据、asmdef 隔离、
// L2 纯净(内核零平台引用)、Adapter 符号在位、测试工程 include 路径不漂移。
package packcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const repoRoot = "../.."

const unityPkgDir = "sdks/unity/packages"

type packageJSON struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	DisplayName string            `json:"displayName"`
	Unity       string            `json:"unity"`
	Description string            `json:"description"`
	Depends     map[string]string `json:"dependencies"`
}

func readJSON(t *testing.T, path string, out interface{}) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}

// 三包目录与依赖链:core(无 SDK 依赖)← service ← ui。
func TestUPMPackages(t *testing.T) {
	wantNames := []string{"com.courier.core", "com.courier.service", "com.courier.ui"}
	for _, name := range wantNames {
		var pkg packageJSON
		readJSON(t, unityPkgDir+"/"+name+"/package.json", &pkg)
		if pkg.Name != name {
			t.Errorf("package.json name = %q, want %q", pkg.Name, name)
		}
		if pkg.Version == "" || pkg.Unity == "" || pkg.DisplayName == "" {
			t.Errorf("%s: version/unity/displayName 必填(name=%q)", name, pkg.Name)
		}
	}

	var service, ui packageJSON
	readJSON(t, unityPkgDir+"/com.courier.service/package.json", &service)
	if service.Depends["com.courier.core"] == "" {
		t.Error("com.courier.service 必须依赖 com.courier.core")
	}
	readJSON(t, unityPkgDir+"/com.courier.ui/package.json", &ui)
	if ui.Depends["com.courier.service"] == "" {
		t.Error("com.courier.ui 必须依赖 com.courier.service")
	}

	var core packageJSON
	readJSON(t, unityPkgDir+"/com.courier.core/package.json", &core)
	if core.Depends["com.unity.nuget.newtonsoft-json"] == "" {
		t.Error("com.courier.core 必须依赖 com.unity.nuget.newtonsoft-json(Json.cs)")
	}
	for name := range core.Depends {
		if strings.HasPrefix(name, "com.courier.") {
			t.Errorf("core 包不得依赖 courier 家族包(L2 无上游),发现 %q", name)
		}
	}
}

// hasRef:asmdef references 是否含指定程序集。
func hasRef(list []string, want string) bool {
	for _, r := range list {
		if r == want {
			return true
		}
	}
	return false
}

// asmdef 隔离:纯 Core 不引平台件;Adapter 显式依赖 Core。
func TestAsmDefIsolation(t *testing.T) {
	var core, adapter struct {
		Name               string   `json:"name"`
		References         []string `json:"references"`
		OverrideReferences bool     `json:"overrideReferences"`
	}
	readJSON(t, unityPkgDir+"/com.courier.core/Runtime/Courier.Core.asmdef", &core)
	if core.Name != "Courier.Core" {
		t.Errorf("asmdef name = %q", core.Name)
	}
	readJSON(t, unityPkgDir+"/com.courier.core/Runtime/Courier.Adapter.asmdef", &adapter)
	if adapter.Name != "Courier.Adapter" {
		t.Errorf("asmdef name = %q", adapter.Name)
	}
	hasRef := func(list []string, want string) bool {
		for _, r := range list {
			if r == want {
				return true
			}
		}
		return false
	}
	if !hasRef(adapter.References, "Courier.Core") {
		t.Error("Courier.Adapter 必须引用 Courier.Core")
	}
	for _, ref := range core.References {
		if strings.HasPrefix(ref, "Courier.") {
			t.Errorf("Courier.Core asmdef 不得引用 Courier.* 程序集,发现 %q", ref)
		}
	}
}

// 批次 6:service/ui 包 Runtime 落位——service 四域件 + SSE 解析器(dotnet 可测),
// UI 双面板(引擎件,结构断言);依赖方向 service→core、ui→service 双重约束;
// service 包延续 L2 纯净(零平台引用),UnityEngine 只许出现在 ui 包。
func TestServiceUiRuntime(t *testing.T) {
	var svcAsm struct {
		Name       string   `json:"name"`
		References []string `json:"references"`
	}
	readJSON(t, unityPkgDir+"/com.courier.service/Runtime/Courier.Service.asmdef", &svcAsm)
	if svcAsm.Name != "Courier.Service" {
		t.Errorf("service asmdef name = %q", svcAsm.Name)
	}
	if !hasRef(svcAsm.References, "Courier.Core") {
		t.Error("Courier.Service 必须引用 Courier.Core")
	}

	var uiAsm struct {
		Name       string   `json:"name"`
		References []string `json:"references"`
	}
	readJSON(t, unityPkgDir+"/com.courier.ui/Runtime/Courier.UI.asmdef", &uiAsm)
	if uiAsm.Name != "Courier.UI" {
		t.Errorf("ui asmdef name = %q", uiAsm.Name)
	}
	if !hasRef(uiAsm.References, "Courier.Service") || !hasRef(uiAsm.References, "Courier.Core") {
		t.Error("Courier.UI 必须引用 Courier.Service + Courier.Core")
	}

	// service 包源码件在位且关键符号齐(域服务/门面/SSE 解析/DTO)。
	svcFiles := map[string][]string{
		"AnnouncementService.cs": {"class AnnouncementService", "ListAsync", "/v1/announcements"},
		"SupportService.cs":      {"class SupportService", "CreateTicketAsync", "AppendMessageAsync", "/v1/support/"},
		"SseParser.cs":           {"class SseParser", "FeedLine", "SseEvent"},
		"ServiceDto.cs":          {"class AnnouncementDto", "class TicketDto", "class FaqDto", "class PageDto"},
		"CourierServices.cs":     {"class CourierServices", "Announcements", "Support", "RealName"},
		"RealNameService.cs":     {"class RealNameService", "SubmitAsync", "CurfewAsync", "/v1/realname"},
		"RealNameMask.cs":        {"MaskName", "MaskIdNumber"},
	}
	svcRuntime := unityPkgDir + "/com.courier.service/Runtime"
	for file, symbols := range svcFiles {
		data, err := os.ReadFile(filepath.Join(repoRoot, svcRuntime, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, sym := range symbols {
			if !strings.Contains(string(data), sym) {
				t.Errorf("service/%s 缺关键符号 %q", file, sym)
			}
		}
		if strings.Contains(string(data), "UnityEngine") || strings.Contains(string(data), "UnityEditor") {
			t.Errorf("service/%s 含平台引用(service 包必须保持 dotnet 可测纯净)", file)
		}
	}

	// ui 包双面板在位:MonoBehaviour 骨架 + 服务门面消费 + Branding 默认标。
	uiFiles := map[string][]string{
		"AnnouncementPanel.cs":    {"class AnnouncementPanel : MonoBehaviour", "CourierServices", "DefaultTitle"},
		"CustomerServicePanel.cs": {"class CustomerServicePanel : MonoBehaviour", "CourierServices", "NotifyTicketReplied"},
	}
	uiRuntime := unityPkgDir + "/com.courier.ui/Runtime"
	for file, symbols := range uiFiles {
		data, err := os.ReadFile(filepath.Join(repoRoot, uiRuntime, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, sym := range symbols {
			if !strings.Contains(string(data), sym) {
				t.Errorf("ui/%s 缺关键符号 %q", file, sym)
			}
		}
	}
}

// L2 纯净:内核(门面/Core/Identity/Session)零平台引用;UnityEngine 只许出现在 Adapter。
func TestL2PurityNoPlatformReferences(t *testing.T) {
	pureDirs := []string{
		unityPkgDir + "/com.courier.core/Runtime",
		unityPkgDir + "/com.courier.core/Runtime/Core",
		unityPkgDir + "/com.courier.core/Runtime/Identity",
		unityPkgDir + "/com.courier.core/Runtime/Session",
	}
	for _, dir := range pureDirs {
		entries, err := os.ReadDir(filepath.Join(repoRoot, dir))
		if err != nil {
			t.Fatalf("read dir %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".cs") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(repoRoot, dir, e.Name()))
			if err != nil {
				t.Fatalf("read %s/%s: %v", dir, e.Name(), err)
			}
			for _, banned := range []string{"UnityEngine", "UnityEditor"} {
				if strings.Contains(string(data), banned) {
					t.Errorf("L2 纯净违规:%s/%s 含 %s(平台引用只许在 Adapter/)", dir, e.Name(), banned)
				}
			}
		}
	}
}

// Adapter 三件在位,且签名对齐 L2 接口(ITransport/ITokenStore/LifecycleMachine.TryFire)。
func TestAdapterSymbols(t *testing.T) {
	adapterDir := unityPkgDir + "/com.courier.core/Runtime/Adapter"
	want := map[string][]string{
		"UnityWebRequestTransport.cs":    {"class UnityWebRequestTransport : ITransport", "UnityWebRequest", "TransportException"},
		"SecureTokenStore.cs":            {"class SecureTokenStore : ITokenStore", "StoredTokens", "HMACSHA256", "Rfc2898DeriveBytes"},
		"ApplicationLifecycleMonitor.cs": {"class ApplicationLifecycleMonitor : MonoBehaviour", "TryFire", "LifecycleTrigger.Suspended", "LifecycleTrigger.ResumeStarted"},
	}
	for file, symbols := range want {
		data, err := os.ReadFile(filepath.Join(repoRoot, adapterDir, file))
		if err != nil {
			t.Fatalf("read %s/%s: %v", adapterDir, file, err)
		}
		for _, sym := range symbols {
			if !strings.Contains(string(data), sym) {
				t.Errorf("%s 缺关键符号 %q", file, sym)
			}
		}
	}
}

// 测试工程 include 路径防漂移:每个 Compile Include 通配必须命中真实文件;不得卷入 Adapter。
func TestTestProjectIncludesResolve(t *testing.T) {
	projects := []string{
		"sdks/unity/CoreTests~/CoreTests.csproj",
		"sdks/unity/ServiceTests~/ServiceTests.csproj",
		"sdks/unity/ContractTests~/ContractTests.csproj",
	}
	for _, proj := range projects {
		data, err := os.ReadFile(filepath.Join(repoRoot, proj))
		if err != nil {
			t.Fatalf("read %s: %v", proj, err)
		}
		content := string(data)
		for _, line := range strings.Split(content, "\n") {
			idx := strings.Index(line, "Include=\"")
			if idx >= 0 && strings.Contains(line, "Adapter") {
				t.Errorf("%s 不得编译 Adapter(UnityEngine 代码,dotnet 编不过)", proj)
			}
			if idx < 0 {
				continue
			}
			pattern := line[idx+len("Include=\""):]
			pattern = pattern[:strings.Index(pattern, "\"")]
			if !strings.Contains(pattern, "*") {
				continue
			}
			base := filepath.Dir(filepath.Join(repoRoot, filepath.Dir(proj), pattern))
			matches, err := filepath.Glob(filepath.Join(repoRoot, filepath.Dir(proj), pattern))
			if err != nil {
				t.Fatalf("glob %s: %v", pattern, err)
			}
			if len(matches) == 0 {
				t.Errorf("%s include %q 未命中任何文件(base=%s)", proj, pattern, base)
			}
		}
	}
}
