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
