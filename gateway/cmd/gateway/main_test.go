package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
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
		resp, body := get("/v1/announcements/list", map[string]string{
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
