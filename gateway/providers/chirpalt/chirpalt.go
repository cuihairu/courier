// Package chirpalt 推送通道第二实现(批次 68,契约 docs/contract/messages.md)。
//
// 与默认 chirp 同一冻结契约,但枢纽模型刻意不同——证明契约约束的是行为
// 而非实现:
//   - chirp(扇出推送):每订阅者独立缓冲 channel,Publish 逐个投递,满即丢
//   - 本实现(共享环形日志 + 游标拉取):发布只追加一次到有界环形日志,
//     每连接各自持游标轮询追进度;落后超出环窗 = 跳过缺失段(at-most-once
//     同语义,与 chirp「满即丢」异构同归——两者都不为慢消费者保底,客户端
//     拉取兜底)。与 archivist-alt「事件日志 vs 状态表」同构的第二实现先例。
//
// 一致性证明:e2e/messages_conformance_test.go——两实现同一 wire 场景同断言
// (批次 18/64/67 先例)。接入方切换 = {"messages":{"primary":"chirp-alt"}}
// 一行配置(域 Provider 的 Notify 钩子在 main 随 primary 同步重指)。
//
// wire 面逐字同 chirp:帧格式 id/event/data、`: connected` 起手、25s `: ping`
// 心跳注释帧、id 连接内单调序号(M2 不做断线补发)。认证经注入的 RequireAuth;
// 未注入 = fail-closed 一律 401(与 wardenalt/archivistalt 同纪律)。
package chirpalt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
)

// DefaultName 注册名;路由表 {"messages":{"primary":"chirp-alt"}} 即换。
const DefaultName = "chirp-alt"

const prefixMessages = "/v1/messages/"

const (
	defaultHeartbeat = 25 * time.Second
	defaultPoll      = 25 * time.Millisecond // 游标拉取间隔(送达延迟上界)
	defaultRingSize  = 256
)

// Options 构造参数(与 chirp.Options 同形状,另加环形日志与拉取参数)。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// RequireAuth 认证中间件(契约:通道为已认证玩家服务);nil = fail-closed。
	RequireAuth func(http.Handler) http.Handler
	// Heartbeat 心跳间隔(契约 25s;测试可调短)。
	Heartbeat time.Duration
	// Poll 游标拉取间隔(测试可调短;默认 25ms)。
	Poll time.Duration
	// RingSize 环形日志容量(超出淘汰最旧;慢消费者落后超出窗口即跳过)。
	RingSize int
}

// entry 已序列化的单条事件(append-only,发布即定稿)。
type entry struct {
	seq  uint64 // 全局单调序号,1 起
	typ  string
	data []byte
}

// Provider 推送通道第二实现(共享环形日志 + 游标拉取)。
type Provider struct {
	name      string
	auth      func(http.Handler) http.Handler
	heartbeat time.Duration
	poll      time.Duration
	ringSize  int

	mu    sync.Mutex
	ring  []entry // 定长循环缓冲(最旧在 head)
	head  int
	count int
	next  uint64 // 下一次发布的全局序号(已发布最大 = next-1)
}

// New 构造推送通道第二实现。
func New(opts Options) *Provider {
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = defaultHeartbeat
	}
	if opts.Poll <= 0 {
		opts.Poll = defaultPoll
	}
	if opts.RingSize <= 0 {
		opts.RingSize = defaultRingSize
	}
	if opts.Name == "" {
		opts.Name = DefaultName
	}
	authMW := func(next http.Handler) http.Handler {
		// fail-closed:未注入认证中间件时拒绝一切(契约:本域全部要求 Bearer)。
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			aggregation.WriteError(w, middleware.TraceID(r.Context()),
				aggregation.CodeUnauthenticated, "not authenticated")
		})
	}
	if opts.RequireAuth != nil {
		authMW = opts.RequireAuth
	}
	return &Provider{
		name:      opts.Name,
		auth:      authMW,
		heartbeat: opts.Heartbeat,
		poll:      opts.Poll,
		ringSize:  opts.RingSize,
		ring:      make([]entry, opts.RingSize),
	}
}

// Name 供应商标识。
func (p *Provider) Name() string { return p.name }

// Capability 能力域:messages。
func (p *Provider) Capability() providers.Capability { return providers.CapMessages }

// HealthCheck 内存环形日志常驻可用。
func (p *Provider) HealthCheck(_ context.Context) error { return nil }

// Publish 追加一条事件到环形日志(main 接线域 Provider 的 Notify 钩子;
// 与 chirp.Hub.Publish 同形状可互换)。序列化失败不发布(域 DTO 均为可
// 序列化结构,此为防御)。
func (p *Provider) Publish(eventType string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ring[(p.head+p.count)%p.ringSize] = entry{seq: p.next + 1, typ: eventType, data: payload}
	p.next++
	if p.count < p.ringSize {
		p.count++
	} else {
		p.head = (p.head + 1) % p.ringSize // 满:淘汰最旧
	}
}

// ServeHTTP 分发 /v1/messages/*(契约仅 stream 一个端点)。
func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.auth(http.HandlerFunc(p.route)).ServeHTTP(w, r)
}

func (p *Provider) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefixMessages)
	switch {
	case path == "stream" && r.Method == http.MethodGet:
		p.handleStream(w, r)
	case path == "stream":
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeInvalidArgument, "stream requires GET")
	default:
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeNotFound, "no route for "+r.URL.Path)
	}
}

// nextBatch 游标推进:取 (cursor, 已发布] 中的可用段;游标落后窗口起点 =
// 跳到窗口起点(at-most-once:缺失段不补发)。cursor 由连接协程独占推进,
// 调用方串行。返回 nil = 暂无新事件。
func (p *Provider) nextBatch(cursor *uint64) []entry {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.count == 0 {
		return nil
	}
	oldest := p.ring[p.head].seq
	if *cursor+1 < oldest { // 落后出窗:跳过缺失段
		*cursor = oldest - 1
	}
	var batch []entry
	for i := 0; i < p.count; i++ {
		e := p.ring[(p.head+i)%p.ringSize]
		if e.seq > *cursor {
			batch = append(batch, e)
			*cursor = e.seq
		}
	}
	return batch
}

// handleStream SSE 长连接(游标拉取循环;帧格式与 chirp 逐字一致)。
func (p *Provider) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeInternal, "streaming unsupported")
		return
	}
	// 订阅点 = 当前已发布末尾:只送订阅后发生的事件(与 chirp 同语义)。
	// 序号 1 起,next = 已发布条数,游标取 next(空日志 = 0,首条序号 1 可投递)。
	p.mu.Lock()
	cursor := p.next
	p.mu.Unlock()
	idSeq := uint64(0) // 连接内单调序号(契约口径;chirp 用全局原子计数,可观察面相同)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // nginx 透传不缓冲
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(p.heartbeat)
	defer heartbeat.Stop()
	poll := time.NewTicker(p.poll)
	defer poll.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-poll.C:
			for _, ev := range p.nextBatch(&cursor) {
				idSeq++
				if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n",
					idSeq, ev.typ, ev.data); err != nil {
					return
				}
			}
			flusher.Flush()
		}
	}
}
