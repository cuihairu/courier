package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// startGateway 在随机空闲端口上启动 main()，等待服务就绪后返回基础 URL。
// 服务随测试进程退出而终止，无需显式关闭。
func startGateway(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("获取空闲端口失败: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("释放端口失败: %v", err)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	t.Setenv("COURIER_GATEWAY_ADDR", addr)
	go main()

	baseURL := "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return baseURL
		}
		if time.Now().After(deadline) {
			t.Fatalf("网关在 %s 上启动超时: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestGatewayCapabilityDegradation 批次 2 端到端验收:空注册表 + 无配置时,
// 已知能力域挂载但返回 501 降级信封;业务名前缀不挂载。
func TestGatewayCapabilityDegradation(t *testing.T) {
	baseURL := startGateway(t)

	get := func(path string, headers map[string]string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
		if err != nil {
			t.Fatalf("构造请求失败: %v", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("请求 %s 失败: %v", path, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("读响应体失败: %v", err)
		}
		return resp, body
	}

	t.Run("能力域未配置 → 501 降级信封", func(t *testing.T) {
		resp, body := get("/v1/realname/verify", map[string]string{
			"X-Courier-Game-Id": "game_demo",
			"X-Courier-Env":     "prod",
		})
		if resp.StatusCode != http.StatusNotImplemented {
			t.Fatalf("状态码 = %d, 期望 501 (body=%s)", resp.StatusCode, body)
		}
		var env struct {
			Error struct {
				Code      string `json:"code"`
				Retryable bool   `json:"retryable"`
			} `json:"error"`
			TraceID string `json:"traceId"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("解析信封失败: %v (body=%s)", err, body)
		}
		if env.Error.Code != "COMMON_CAPABILITY_DISABLED" || env.Error.Retryable {
			t.Errorf("error = %+v, 期望 COMMON_CAPABILITY_DISABLED retryable=false", env.Error)
		}
		if len(env.TraceID) != 32 {
			t.Errorf("traceId = %q, 期望 32-hex", env.TraceID)
		}
	})

	t.Run("缺 scope → 400 COMMON_INVALID_ARGUMENT", func(t *testing.T) {
		resp, body := get("/v1/app/config", nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("状态码 = %d, 期望 400 (body=%s)", resp.StatusCode, body)
		}
	})

	t.Run("业务名前缀不挂载 → 404", func(t *testing.T) {
		resp, body := get("/v1/accounts/list", nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("状态码 = %d, 期望 404 (body=%s)", resp.StatusCode, body)
		}
		var env struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("解析信封失败: %v (body=%s)", err, body)
		}
		if env.Error.Code != "COMMON_NOT_FOUND" {
			t.Errorf("error.code = %q, 期望 COMMON_NOT_FOUND", env.Error.Code)
		}
	})
}

func TestGatewayHealthz(t *testing.T) {
	baseURL := startGateway(t)

	t.Run("GET 返回 ok 状态", func(t *testing.T) {
		resp, err := http.Get(baseURL + "/healthz")
		if err != nil {
			t.Fatalf("请求 /healthz 失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("状态码 = %d, 期望 %d", resp.StatusCode, http.StatusOK)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, 期望 %q", ct, "application/json")
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("读取响应体失败: %v", err)
		}
		var got map[string]string
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("解析响应 JSON 失败: %v (body=%q)", err, body)
		}
		if got["status"] != "ok" {
			t.Errorf("status = %q, 期望 %q (body=%q)", got["status"], "ok", body)
		}
	})

	t.Run("非 GET 方法返回 405", func(t *testing.T) {
		resp, err := http.Post(baseURL+"/healthz", "text/plain", nil)
		if err != nil {
			t.Fatalf("POST /healthz 失败: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("状态码 = %d, 期望 %d", resp.StatusCode, http.StatusMethodNotAllowed)
		}
	})
}

// --- 批次 3 e2e:roadmap M1 服务端验收(结构化错误、限流生效) ---

// envelope 通用信封。
type envelope struct {
	Error *struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Retryable bool   `json:"retryable"`
	} `json:"error"`
	TraceID string          `json:"traceId"`
	Data    json.RawMessage `json:"data"`
}

// postJSON 向网关发 JSON 请求并解析信封。
func postJSON(t *testing.T, baseURL, path, body, bearer string) (int, envelope, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Courier-Game-Id", "game_e2e")
	req.Header.Set("X-Courier-Env", "prod")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s 失败: %v", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读响应失败: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("响应非 JSON: %v (body=%s)", err, raw)
	}
	return resp.StatusCode, env, resp.Header
}

// sessionData 解析会话 data。
type e2eSession struct {
	Account struct {
		ID    string  `json:"id"`
		Type  string  `json:"type"`
		Email *string `json:"email"`
	} `json:"account"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	DeviceID     string `json:"deviceId"`
}

func e2eSessionOf(t *testing.T, env envelope) e2eSession {
	t.Helper()
	var s e2eSession
	if err := json.Unmarshal(env.Data, &s); err != nil {
		t.Fatalf("解析会话失败: %v (data=%s)", err, env.Data)
	}
	return s
}

// TestGatewayIdentityFlow M1 全链路(服务端版):游客登录 → 绑定邮箱 → 登出 →
// 登录 → refresh 轮换 → 重放检测吊销 → 吊销后拒绝;全程错误为结构化 JSON。
func TestGatewayIdentityFlow(t *testing.T) {
	baseURL := startGateway(t)

	t.Run("游客登录", func(t *testing.T) {
		status, env, _ := postJSON(t, baseURL, "/v1/identity/guest", `{"deviceId":"e2e-dev","platform":"ios"}`, "")
		if status != http.StatusOK {
			t.Fatalf("status = %d (error=%+v)", status, env.Error)
		}
		s := e2eSessionOf(t, env)
		if s.Account.Type != "GUEST" || s.DeviceID != "e2e-dev" {
			t.Fatalf("account=%+v deviceId=%q", s.Account, s.DeviceID)
		}
	})

	// 重新 guest 拿一个会话做绑定(上一子测试会话不跨子测试保存)——直接再登一次
	status, env, _ := postJSON(t, baseURL, "/v1/identity/guest", `{"deviceId":"e2e-dev"}`, "")
	if status != http.StatusOK {
		t.Fatalf("游客重登失败: %d", status)
	}
	guest := e2eSessionOf(t, env)

	t.Run("绑定邮箱(转正)", func(t *testing.T) {
		status, env, _ := postJSON(t, baseURL, "/v1/identity/bind",
			`{"email":"e2e@b.co","password":"password8"}`, guest.AccessToken)
		if status != http.StatusOK {
			t.Fatalf("status = %d (error=%+v)", status, env.Error)
		}
		var acc struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(env.Data, &acc); err != nil {
			t.Fatalf("解析账号失败: %v (data=%s)", err, env.Data)
		}
		if acc.Type != "EMAIL" {
			t.Fatalf("转正后 type=%q", acc.Type)
		}
	})

	t.Run("登出 → 吊销后拒绝", func(t *testing.T) {
		// 用绑定后的新登录拿会话
		st, env2, _ := postJSON(t, baseURL, "/v1/identity/login",
			`{"email":"e2e@b.co","password":"password8","deviceId":"e2e-dev"}`, "")
		if st != http.StatusOK {
			t.Fatalf("登录失败: %d (error=%+v)", st, env2.Error)
		}
		s := e2eSessionOf(t, env2)

		st, _, _ = postJSON(t, baseURL, "/v1/identity/logout", `{}`, s.AccessToken)
		if st != http.StatusOK {
			t.Fatalf("logout status = %d", st)
		}
		st, env3, _ := postJSON(t, baseURL, "/v1/identity/session", "", s.AccessToken)
		if st != http.StatusUnauthorized || env3.Error == nil || env3.Error.Code != "AUTH_TOKEN_REVOKED" {
			t.Fatalf("吊销后: %d %+v", st, env3.Error)
		}
	})

	t.Run("refresh 轮换 + 重放检测", func(t *testing.T) {
		st, env, _ := postJSON(t, baseURL, "/v1/identity/login",
			`{"email":"e2e@b.co","password":"password8","deviceId":"e2e-dev"}`, "")
		if st != http.StatusOK {
			t.Fatalf("登录失败: %d", st)
		}
		s := e2eSessionOf(t, env)

		st, env2, _ := postJSON(t, baseURL, "/v1/identity/refresh",
			`{"refreshToken":"`+s.RefreshToken+`"}`, "")
		if st != http.StatusOK {
			t.Fatalf("refresh status = %d (error=%+v)", st, env2.Error)
		}
		s2 := e2eSessionOf(t, env2)
		if s2.RefreshToken == s.RefreshToken {
			t.Fatal("轮换应生成新 refresh")
		}
		// 重放旧 refresh → REUSED
		st, env3, _ := postJSON(t, baseURL, "/v1/identity/refresh",
			`{"refreshToken":"`+s.RefreshToken+`"}`, "")
		if st != http.StatusUnauthorized || env3.Error == nil || env3.Error.Code != "AUTH_REFRESH_REUSED" {
			t.Fatalf("重放: %d %+v", st, env3.Error)
		}
		// 会话被吊销:新 refresh 死
		st, env4, _ := postJSON(t, baseURL, "/v1/identity/refresh",
			`{"refreshToken":"`+s2.RefreshToken+`"}`, "")
		if st != http.StatusUnauthorized || env4.Error == nil || env4.Error.Code != "AUTH_TOKEN_REVOKED" {
			t.Fatalf("吊销后: %d %+v", st, env4.Error)
		}
	})

	t.Run("错误为结构化 JSON(code/message/traceId)", func(t *testing.T) {
		st, env, _ := postJSON(t, baseURL, "/v1/identity/login",
			`{"email":"e2e@b.co","password":"wrong-pass-9"}`, "")
		if st != http.StatusUnauthorized {
			t.Fatalf("status = %d", st)
		}
		if env.Error == nil || env.Error.Code != "AUTH_INVALID_CREDENTIALS" ||
			env.Error.Retryable || env.Error.Message == "" || len(env.TraceID) != 32 {
			t.Fatalf("结构化错误不完整: %+v traceId=%q", env.Error, env.TraceID)
		}
	})
}

// TestGatewayLoginRateLimit M1 验收:并发登录限流生效(默认每 IP 10/min)。
func TestGatewayLoginRateLimit(t *testing.T) {
	baseURL := startGateway(t)

	// 前 10 次进入业务路径(401),第 11 次被限流(429 RATE_LIMITED + Retry-After)。
	var lastHeader http.Header
	for i := 1; i <= 11; i++ {
		req, _ := http.NewRequest(http.MethodPost, baseURL+"/v1/identity/login", strings.NewReader(
			`{"email":"flood@b.co","password":"whatever123"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Courier-Game-Id", "game_e2e")
		req.Header.Set("X-Courier-Env", "prod")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("第 %d 次请求失败: %v", i, err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var env envelope
		_ = json.Unmarshal(raw, &env)

		if i <= 10 {
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("第 %d 次: status=%d body=%s, 期望 401", i, resp.StatusCode, raw)
			}
			continue
		}
		lastHeader = resp.Header.Clone()
		if resp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("第 %d 次: status=%d body=%s, 期望 429", i, resp.StatusCode, raw)
		}
		if env.Error == nil || env.Error.Code != "RATE_LIMITED" || !env.Error.Retryable {
			t.Fatalf("限流错误异常: %+v", env.Error)
		}
	}
	if lastHeader.Get("Retry-After") == "" {
		t.Fatal("429 应带 Retry-After")
	}
}
