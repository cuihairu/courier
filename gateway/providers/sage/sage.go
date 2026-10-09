// Package sage 小助手默认 Provider(M4,契约 docs/contract/assistant.md)。
//
// 能力域 assistant(/v1/assistant/*),玩家侧:
//
//	POST /v1/assistant/query   Bearer;知识库检索问答
//
// 命中 → 返回知识条目原样快照;未命中 → 200 + matched:false + suggestTransfer:true
// (未命中不是错误,与 support FAQ 检索空结果同哲学)。
// 知识库登记走管理面(本包导出 AddEntry),与 herald/croupier 同模式;本域**无推送事件**。
// LLM 接口预留但默认不依赖:v1 实现为检索(零外部依赖、答案可审计不生成);
// 接入方按同一 Provider 接口换 LLM 实现,契约语义不变。
package sage

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
)

// DefaultName 注册名(main.go 默认路由表指向它)。
const DefaultName = "sage"

const prefixAssistant = "/v1/assistant/"

// 契约上限(assistant.md「POST /v1/assistant/query」)。
const maxQueryRunes = 500

// Options 构造参数。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// RequireAuth 认证中间件(main 注入 auth.RequireAuth(verifier));
	// nil = fail-closed(一律 401):忘注入不能变成裸奔。
	RequireAuth func(http.Handler) http.Handler
}

// entry 知识条目(契约:命中即返回原样快照,不改写)。
type entry struct {
	ID       string
	Question string
	Answer   string
	Keywords []string
}

// AssistantProvider 基于检索的小助手。
type AssistantProvider struct {
	name string
	auth func(http.Handler) http.Handler

	mu      sync.RWMutex
	entries []entry // 登记序;检索按命中强度折算
	queries int64   // 管理面统计(roadmap M4 验收:命中率可统计);内存计数,重启归零
	hits    int64
}

// New 构造小助手 Provider。
func New(opts Options) *AssistantProvider {
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
	return &AssistantProvider{
		name: opts.Name,
		auth: authMW,
	}
}

// Name 供应商标识。
func (p *AssistantProvider) Name() string { return p.name }

// Capability 能力域:assistant。
func (p *AssistantProvider) Capability() providers.Capability { return providers.CapAssistant }

// HealthCheck 内存知识库常驻可用。
func (p *AssistantProvider) HealthCheck(_ context.Context) error { return nil }

// AddEntry 管理面:新增知识条目(登记序即检索序)。
// 返回条目快照,便于管理面回显。
func (p *AssistantProvider) AddEntry(question, answer string, keywords []string) entry {
	p.mu.Lock()
	defer p.mu.Unlock()
	e := entry{
		ID:       "faq_" + newID(len(p.entries), question),
		Question: question,
		Answer:   answer,
		Keywords: keywords,
	}
	p.entries = append(p.entries, e)
	return e
}

// EntryCount 管理面:知识库条数(测试/运维)。
func (p *AssistantProvider) EntryCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.entries)
}

// QueryStats 管理面:检索量与命中率(roadmap M4「常见问题命中率可统计」;
// 进程内计数,持久化与跨实例聚合归部署面)。
func (p *AssistantProvider) QueryStats() (queries, hits int64) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.queries, p.hits
}

// ServeHTTP 分发 /v1/assistant/*(全部 Bearer)。
func (p *AssistantProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.auth(http.HandlerFunc(p.route)).ServeHTTP(w, r)
}

func (p *AssistantProvider) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefixAssistant)
	switch {
	case path == "query" && r.Method == http.MethodPost:
		p.handleQuery(w, r)
	default:
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeNotFound, "no route for "+r.URL.Path)
	}
}

func (p *AssistantProvider) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		p.writeInvalid(w, r, "body 须为 JSON")
		return
	}
	text := strings.TrimSpace(req.Text)
	if n := len([]rune(text)); n < 1 || n > maxQueryRunes {
		p.writeInvalid(w, r, "text 须为 1-500 字符(修剪首尾后)")
		return
	}

	best, ok := p.bestMatch(text)
	p.mu.Lock()
	p.queries++
	if ok {
		p.hits++
	}
	p.mu.Unlock()
	if !ok {
		// 未命中不是错误:200 + matched:false + suggestTransfer:true。
		aggregation.WriteData(w, queryDTO{Matched: false, SuggestTransfer: true})
		return
	}
	aggregation.WriteData(w, queryDTO{
		Matched:         true,
		Answer:          &answerDTO{ID: best.ID, Question: best.Question, Answer: best.Answer, Keywords: best.Keywords},
		SuggestTransfer: false,
	})
}

// bestMatch 检索评分(实现细节,不进契约):命中条目返回最高分;全不命中 = ok=false。
// 评分:问题子串命中 3 分,关键词命中 2 分,答案子串命中 1 分;同分取登记序靠前者。
func (p *AssistantProvider) bestMatch(text string) (entry, bool) {
	q := strings.ToLower(text)
	p.mu.RLock()
	defer p.mu.RUnlock()
	bestScore := 0
	var best entry
	for _, e := range p.entries {
		score := 0
		if contains(q, e.Question) {
			score += 3
		}
		for _, k := range e.Keywords {
			if k != "" && contains(q, k) {
				score += 2
				break
			}
		}
		if contains(q, e.Answer) {
			score++
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best, bestScore > 0
}

// contains 双向包含:查询含条目文本,或条目含查询词(短问长答双向命中)。
func contains(queryLower, target string) bool {
	t := strings.ToLower(strings.TrimSpace(target))
	if t == "" {
		return false
	}
	return strings.Contains(queryLower, t) || strings.Contains(t, queryLower)
}

func (p *AssistantProvider) writeInvalid(w http.ResponseWriter, r *http.Request, msg string) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeInvalidArgument, msg)
}

// newID 稳定条目 ID(无外部依赖;登记序 + 问题指纹)。
func newID(seq int, question string) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	h := uint64(1469598103934665603)
	for i := 0; i < len(question); i++ {
		h ^= uint64(question[i])
		h *= 1099511628211
	}
	var b [8]byte
	for i := 7; i >= 0; i-- {
		b[i] = alphabet[h%uint64(len(alphabet))]
		h /= uint64(len(alphabet))
	}
	return string(b[:]) + strconv.Itoa(seq)
}

// wire DTO(契约数据模型,camelCase;未命中时 answer 缺省不下发)。

type answerDTO struct {
	ID       string   `json:"id"`
	Question string   `json:"question"`
	Answer   string   `json:"answer"`
	Keywords []string `json:"keywords,omitempty"`
}

type queryDTO struct {
	Matched         bool       `json:"matched"`
	Answer          *answerDTO `json:"answer,omitempty"`
	SuggestTransfer bool       `json:"suggestTransfer"`
}
