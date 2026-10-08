// Package chirp 推送通道默认 Provider(M2,契约 docs/contract/messages.md)。
//
// 能力域 messages(/v1/messages/stream),SSE(text/event-stream):
// 单连接承载全部域事件(会话复用)、at-most-once(订阅缓冲满即丢,客户端拉取兜底)、
// 25s 心跳注释帧保活、id 为连接内单调序号(M2 不做断线补发)。
// 认证经注入的 RequireAuth(main 用 auth.RequireAuth(verifier));Provider 不感知会话实现。
package chirp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
)

// DefaultName 注册名(main.go 默认路由表指向它)。
const DefaultName = "chirp"

const prefixMessages = "/v1/messages/"

const (
	defaultHeartbeat = 25 * time.Second
	defaultBuffer    = 64
)

// Options 构造参数。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// RequireAuth 认证中间件(契约:通道为已认证玩家服务,匿名 401)。
	RequireAuth func(http.Handler) http.Handler
	// Heartbeat 心跳间隔(契约 25s;测试可调短)。
	Heartbeat time.Duration
	// SubBuffer 单订阅者缓冲(满即丢该订阅者的此条事件:at-most-once)。
	SubBuffer int
}

// MessageProvider SSE 推送通道。
type MessageProvider struct {
	name      string
	hub       *Hub
	auth      func(http.Handler) http.Handler
	heartbeat time.Duration
	buffer    int
}

// New 构造推送通道 Provider。
func New(opts Options) *MessageProvider {
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = defaultHeartbeat
	}
	if opts.SubBuffer <= 0 {
		opts.SubBuffer = defaultBuffer
	}
	if opts.Name == "" {
		opts.Name = DefaultName
	}
	pass := func(h http.Handler) http.Handler { return h }
	if opts.RequireAuth != nil {
		pass = opts.RequireAuth
	}
	return &MessageProvider{
		name:      opts.Name,
		hub:       NewHub(opts.SubBuffer),
		auth:      pass,
		heartbeat: opts.Heartbeat,
		buffer:    opts.SubBuffer,
	}
}

// Hub 暴露事件枢纽(main/herald/croupier 装配 Notify 用)。
func (p *MessageProvider) Hub() *Hub { return p.hub }

// Hub 事件枢纽:域 Provider 经 Notify 钩子发布,流订阅者消费。
type Hub struct {
	mu   sync.RWMutex
	subs map[*subscriber]struct{}
	seq  atomic.Int64
}

type subscriber struct {
	ch chan outbound
}

// outbound 已序列化的单条事件帧载荷。
type outbound struct {
	typ  string
	data []byte
}

// NewHub 空枢纽(buffer 为单订阅者缓冲)。
func NewHub(buffer int) *Hub {
	if buffer <= 0 {
		buffer = defaultBuffer
	}
	return &Hub{subs: make(map[*subscriber]struct{})}
}

// Publish 广播一条事件。缓冲满的订阅者直接丢弃该条(at-most-once,契约语义;
// 客户端以拉取兜底,通道是优化不是依赖)。
func (h *Hub) Publish(eventType string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return // data 序列化失败不广播(域 DTO 均为可序列化结构,此为防御)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for sub := range h.subs {
		select {
		case sub.ch <- outbound{typ: eventType, data: payload}:
		default: // 满:丢
		}
	}
}

// Name 供应商标识。
func (p *MessageProvider) Name() string { return p.name }

// Capability 能力域:messages。
func (p *MessageProvider) Capability() providers.Capability { return providers.CapMessages }

// HealthCheck 内存枢纽常驻可用。
func (p *MessageProvider) HealthCheck(_ context.Context) error { return nil }

// ServeHTTP 分发 /v1/messages/*(契约仅 stream 一个端点)。
func (p *MessageProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefixMessages)
	switch {
	case path == "stream" && r.Method == http.MethodGet:
		p.auth(http.HandlerFunc(p.handleStream)).ServeHTTP(w, r)
	case path == "stream":
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeInvalidArgument, "stream requires GET")
	default:
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeNotFound, "no route for "+r.URL.Path)
	}
}

// handleStream SSE 长连接。
func (p *MessageProvider) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeInternal, "streaming unsupported")
		return
	}
	sub := &subscriber{ch: make(chan outbound, p.buffer)}
	p.hub.mu.Lock()
	p.hub.subs[sub] = struct{}{}
	p.hub.mu.Unlock()
	defer func() {
		p.hub.mu.Lock()
		delete(p.hub.subs, sub)
		p.hub.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx 透传不缓冲
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ticker := time.NewTicker(p.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev := <-sub.ch:
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n",
				p.hub.seq.Add(1), ev.typ, ev.data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
