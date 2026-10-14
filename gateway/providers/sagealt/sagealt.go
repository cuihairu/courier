// Package sagealt 小助手第二实现(批次 70,契约 docs/contract/assistant.md)。
//
// 与默认 sage 同一冻结契约,但检索预处理刻意不同——证明契约约束的是行为
// 而非实现(assistant.md:「命中评分规则、同分取舍、分词方式均为实现细节,
// 不进契约」):
//   - sage(读时计算):每次查询对每个条目重复 ToLower/Trim 规范化
//   - 本实现(写时预计算):AddEntry 时预计算规范化文本,查询匹配零重复计算
//
// 规范化幂等(ToLower 不产生空白、TrimSpace 不改大小写 ⇒ norm(norm(s)) =
// norm(s)),故两实现命中集逐条一致;评分权重(3/2/1)、同分取舍(登记序
// 靠前)、双向包含语义与 sage 同源。wire 快照仍取原始文本(契约「原样
// 快照,不改写」),规范化只服务匹配。
//
// 一致性证明:e2e/assistant_conformance_test.go——两实现同一 wire 场景同
// 断言(批次 18/64/67/68/69 先例)。接入方切换 =
// {"assistant":{"primary":"sage-alt"}} 一行配置。
//
// newID 与 sage 包同构实现(跨包不引私有件,仓内先例;一致性由契约测试守护)。
package sagealt

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

// DefaultName 注册名;路由表 {"assistant":{"primary":"sage-alt"}} 即换。
const DefaultName = "sage-alt"

const prefixAssistant = "/v1/assistant/"

// 契约上限(assistant.md「POST /v1/assistant/query」)。
const maxQueryRunes = 500

// Options 构造参数(与 sage.Options 同形状)。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// RequireAuth 认证中间件;nil = fail-closed(一律 401)。
	RequireAuth func(http.Handler) http.Handler
}

// entry 知识条目:原始文本供 wire 原样快照,规范化文本供匹配零重复计算。
type entry struct {
	ID       string
	Question string
	Answer   string
	Keywords []string

	normQuestion string
	normAnswer   string
	normKeywords []string // 与 Keywords 等长同序;元素 = normalize(原值)
}

// AssistantProvider 检索问答第二实现(写时预计算规范化文本)。
type AssistantProvider struct {
	name string
	auth func(http.Handler) http.Handler

	mu      sync.RWMutex
	entries []entry // 登记序;检索按命中强度折算(与 sage 同)
	queries int64   // 管理面统计(与 sage 同:进程内计数,重启归零)
	hits    int64
}

// New 构造小助手 Provider 第二实现。
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

// AddEntry 管理面:新增知识条目(登记序即检索序)。写时预计算规范化文本
// (异构面:sage 在每次查询时对同一条目重复规范化,本实现只做一次)。
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
	e.normQuestion = normalize(question)
	e.normAnswer = normalize(answer)
	if keywords != nil {
		e.normKeywords = make([]string, len(keywords))
		for i, k := range keywords {
			e.normKeywords[i] = normalize(k)
		}
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

// QueryStats 管理面:检索量与命中率(与 sage 同语义)。
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
	// wire 快照取原始文本(契约:原样返回,不改写)。
	aggregation.WriteData(w, queryDTO{
		Matched:         true,
		Answer:          &answerDTO{ID: best.ID, Question: best.Question, Answer: best.Answer, Keywords: best.Keywords},
		SuggestTransfer: false,
	})
}

// bestMatch 检索评分(实现细节,不进契约):与 sage 同一权重(问题 3 /
// 关键词 2 / 答案 1)、同一同分取舍(登记序靠前)、同一双向包含语义;
// 唯一差别 = 读预计算的规范化字段,查询路径上零规范化计算。
func (p *AssistantProvider) bestMatch(text string) (entry, bool) {
	q := strings.ToLower(text)
	p.mu.RLock()
	defer p.mu.RUnlock()
	bestScore := 0
	var best entry
	for _, e := range p.entries {
		score := 0
		if containsNorm(q, e.normQuestion) {
			score += 3
		}
		for _, k := range e.normKeywords {
			if k != "" && containsNorm(q, k) {
				score += 2
				break
			}
		}
		if containsNorm(q, e.normAnswer) {
			score++
		}
		if score > bestScore {
			bestScore = score
			best = e
		}
	}
	return best, bestScore > 0
}

// normalize 规范化(TrimSpace + ToLower)。幂等:ToLower 不把非空白映射成
// 空白、TrimSpace 不改大小写 ⇒ normalize(normalize(s)) = normalize(s)。
// 故 sage 的「读时对原值再规范化」与本实现的「读写时规范化值」等价。
func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// containsNorm 双向包含(与 sage.contains 同语义,target 已是规范化形式):
// 查询含条目文本,或条目含查询词(短问长答双向命中)。
func containsNorm(queryLower, target string) bool {
	if target == "" {
		return false
	}
	return strings.Contains(queryLower, target) || strings.Contains(target, queryLower)
}

func (p *AssistantProvider) writeInvalid(w http.ResponseWriter, r *http.Request, msg string) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeInvalidArgument, msg)
}

// newID 稳定条目 ID(与 sage 同构:无外部依赖;登记序 + 问题指纹)。
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
