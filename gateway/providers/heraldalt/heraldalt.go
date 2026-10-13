// Package heraldalt 公告第二实现(批次 69,契约 docs/contract/announcement.md)。
//
// 与默认 herald 同一冻结契约,但存储模型刻意不同——证明契约约束的是行为
// 而非实现:
//   - herald(读时计算):单切片 append,列表读时线性扫描过滤 + 读时排序
//   - 本实现(写时索引):map 按 ID 索引(详情 O(1))+ 发布时插入维护
//     publishedAt 升序切片(列表倒序遍历即契约倒序输出,无读时排序)
//
// 可见期/filters 定向匹配/分页/发布校验逐字同源(契约行为);一致性证明:
// e2e/announcements_conformance_test.go——两实现同一 wire 场景同断言
// (批次 18/64/67/68 先例)。接入方切换 =
// {"announcements":{"primary":"herald-alt"}} 一行配置。
//
// UUIDv7 生成器与 herald/account 包同构实现(跨包不引私有件,仓内先例;
// 一致性由契约测试守护)。
package heraldalt

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
)

// DefaultName 注册名;路由表 {"announcements":{"primary":"herald-alt"}} 即换。
const DefaultName = "herald-alt"

// EventPublished 推送事件 type(契约 messages.md 事件登记,与 herald 同值)。
const EventPublished = "announcement.published"

const prefixAnnouncements = "/v1/announcements/"

// Options 构造参数(与 herald.Options 同形状)。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// Clock 当前时间(可见期判定与发布时间;默认 time.Now)。
	Clock func() time.Time
	// RequireAuth 认证中间件;nil = fail-closed(一律 401)。
	RequireAuth func(http.Handler) http.Handler
	// Notify 推送钩子(announcement.published,参数为 announcementDTO);nil = 不推送。
	Notify func(eventType string, data any)
}

// Announcement 管理面实体(与 herald 同形)。
type Announcement struct {
	ID          string
	Title       string
	Body        string
	Severity    string
	StartAt     *time.Time
	EndAt       *time.Time
	PublishedAt time.Time
	Filters     map[string][]string
	Language    string
	GroupKey    string
}

// announcementDTO wire 形状(契约数据模型,camelCase;缺省边界输出 null)。
type announcementDTO struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	Body        string              `json:"body"`
	Severity    string              `json:"severity"`
	StartAt     *string             `json:"startAt"`
	EndAt       *string             `json:"endAt"`
	PublishedAt string              `json:"publishedAt"`
	Filters     map[string][]string `json:"filters,omitempty"`
	Language    string              `json:"language,omitempty"` // F76,语种声明元数据
	GroupKey    string              `json:"groupKey,omitempty"` // F76,多语条目关联(客户端不解释)
}

type pageDTO struct {
	Items      []announcementDTO `json:"items"`
	NextCursor string            `json:"nextCursor"`
}

// Provider 公告只读投影第二实现(写时索引模型)。
type Provider struct {
	name   string
	clock  func() time.Time
	auth   func(http.Handler) http.Handler
	notify func(eventType string, data any)

	mu      sync.RWMutex
	byID    map[string]Announcement // 详情 O(1) 索引
	ordered []Announcement          // publishedAt 升序(同刻 ID 升序);倒序遍历 = 契约输出序
	nextID  int
}

// New 构造公告 Provider 第二实现。
func New(opts Options) *Provider {
	if opts.Name == "" {
		opts.Name = DefaultName
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
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
		name:   opts.Name,
		clock:  opts.Clock,
		auth:   authMW,
		notify: opts.Notify,
		byID:   make(map[string]Announcement),
		nextID: 1,
	}
}

// Name 供应商标识。
func (p *Provider) Name() string { return p.name }

// Capability 能力域:announcements。
func (p *Provider) Capability() providers.Capability { return providers.CapAnnouncements }

// HealthCheck 内存存储常驻可用。
func (p *Provider) HealthCheck(_ context.Context) error { return nil }

// Publish 管理面发布(运营侧):校验逐字同源 herald、分配 ann_ ID、写时入索引、广播。
func (p *Provider) Publish(title, body, severity string, startAt, endAt *time.Time,
	filters map[string][]string, language, groupKey string) (Announcement, error) {
	if title == "" || len([]rune(title)) > 120 {
		return Announcement{}, errInvalid("title 1-120 字符")
	}
	if body == "" || len([]rune(body)) > 4000 {
		return Announcement{}, errInvalid("body 1-4000 字符")
	}
	switch severity {
	case "INFO", "WARNING", "CRITICAL":
	default:
		return Announcement{}, errInvalid("severity 须为 INFO/WARNING/CRITICAL")
	}
	if startAt != nil && endAt != nil && endAt.Before(*startAt) {
		return Announcement{}, errInvalid("endAt 早于 startAt")
	}

	p.mu.Lock()
	a := Announcement{
		ID:          "ann_" + newUUIDv7(p.clock()),
		Title:       title,
		Body:        body,
		Severity:    severity,
		StartAt:     startAt,
		EndAt:       endAt,
		PublishedAt: p.clock(),
		Filters:     filters,
		Language:    language,
		GroupKey:    groupKey,
	}
	p.byID[a.ID] = a
	// 写时索引:插入维护 publishedAt 升序(同刻 ID 升序)——倒序遍历即
	// herald 读时排序的输出序(publishedAt 倒序、同刻 ID 倒序)。
	i := sort.Search(len(p.ordered), func(i int) bool {
		o := p.ordered[i]
		if o.PublishedAt.Equal(a.PublishedAt) {
			return o.ID > a.ID
		}
		return o.PublishedAt.After(a.PublishedAt)
	})
	p.ordered = append(p.ordered, Announcement{})
	copy(p.ordered[i+1:], p.ordered[i:])
	p.ordered[i] = a
	p.nextID++
	p.mu.Unlock()

	if p.notify != nil {
		p.notify(EventPublished, p.dto(a))
	}
	return a, nil
}

// ServeHTTP 分发 /v1/announcements/*(只读,全部要求 Bearer)。
func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.auth(http.HandlerFunc(p.route)).ServeHTTP(w, r)
}

func (p *Provider) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefixAnnouncements)
	if path == "" || path == "/" {
		if r.Method == http.MethodGet {
			p.handleList(w, r)
			return
		}
		p.writeInvalid(w, r, "list requires GET")
		return
	}
	if !strings.Contains(path, "/") {
		if r.Method == http.MethodGet {
			p.handleDetail(w, r, path)
			return
		}
		p.writeInvalid(w, r, "detail requires GET")
		return
	}
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeNotFound, "no route for "+r.URL.Path)
}

// handleList 倒序遍历写时有序切片:跳过 cursor 个可见项、取 limit 个;
// 取满后仍有可见项 → next cursor(与 herald 先收集再切片的输出逐条一致)。
func (p *Provider) handleList(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pageParams(r)
	if err != nil {
		p.writeInvalid(w, r, err.Error())
		return
	}
	dims := filterDims(r)
	now := p.clock()
	p.mu.RLock()
	items := make([]announcementDTO, 0, limit)
	next := ""
	seen := 0
	for i := len(p.ordered) - 1; i >= 0; i-- {
		a := p.ordered[i]
		if !a.visible(now) || !a.visibleTo(dims) {
			continue
		}
		if seen < cursor {
			seen++
			continue
		}
		if len(items) < limit {
			items = append(items, p.dto(a))
			continue
		}
		next = strconv.Itoa(cursor + limit) // 取满仍有可见项:还有下一页
		break
	}
	p.mu.RUnlock()
	aggregation.WriteData(w, pageDTO{Items: items, NextCursor: next})
}

// handleDetail map 索引 O(1);不存在/过期/未到 startAt/被定向过滤排除
// 一律同码:不泄露存在性(契约)。
func (p *Provider) handleDetail(w http.ResponseWriter, r *http.Request, id string) {
	dims := filterDims(r)
	now := p.clock()
	p.mu.RLock()
	a, ok := p.byID[id]
	p.mu.RUnlock()
	if !ok || !a.visible(now) || !a.visibleTo(dims) {
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeAnnouncementNotFound, "announcement not found")
		return
	}
	aggregation.WriteData(w, p.dto(a))
}

func (p *Provider) writeInvalid(w http.ResponseWriter, r *http.Request, msg string) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeInvalidArgument, msg)
}

func (a Announcement) visible(now time.Time) bool {
	if a.PublishedAt.After(now) {
		return false
	}
	if a.StartAt != nil && now.Before(*a.StartAt) {
		return false
	}
	if a.EndAt != nil && now.After(*a.EndAt) {
		return false
	}
	return true
}

// filterDims 请求携带的定向维度(契约 v1.1:platform/appVersion/region/language;
// 未知查询参数容忍忽略,未知维度 key 是管理面开集,匹配走同一规则)。
func filterDims(r *http.Request) map[string]string {
	q := r.URL.Query()
	dims := make(map[string]string, 4)
	for _, k := range []string{"platform", "appVersion", "region", "language"} {
		if v := q.Get(k); v != "" {
			dims[k] = v
		}
	}
	return dims
}

// visibleTo 定向匹配(契约 v1.1):filters 中出现的每个维度,若请求携带对应
// 参数且值不在集合内 → 不可见;请求缺该维度参数 → 该维度不排除(缺省跳过)。
func (a Announcement) visibleTo(dims map[string]string) bool {
	for dim, allowed := range a.Filters {
		if len(allowed) == 0 {
			continue // 空集合 = 维度未生效(缺省跳过语义)
		}
		if v, ok := dims[dim]; ok && !containsStr(allowed, v) {
			return false
		}
	}
	return true
}

func containsStr(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

func (p *Provider) dto(a Announcement) announcementDTO {
	return announcementDTO{
		ID:          a.ID,
		Title:       a.Title,
		Body:        a.Body,
		Severity:    a.Severity,
		StartAt:     formatMilliPtr(a.StartAt),
		EndAt:       formatMilliPtr(a.EndAt),
		PublishedAt: formatMilli(a.PublishedAt),
		Filters:     a.Filters,
		Language:    a.Language,
		GroupKey:    a.GroupKey,
	}
}

// pageParams 解析 limit(默认 20,最大 100)与 cursor(offset 序号;空 = 0)。
func pageParams(r *http.Request) (limit, cursor int, err error) {
	limit = 20
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, errInvalid("limit 须为 1-100")
		}
	}
	if v := r.URL.Query().Get("cursor"); v != "" {
		cursor, err = strconv.Atoi(v)
		if err != nil || cursor < 0 {
			return 0, 0, errInvalid("cursor 非法")
		}
	}
	return limit, cursor, nil
}

// errInvalid 参数类错误(信封 COMMON_INVALID_ARGUMENT)。
type invalidError string

func (e invalidError) Error() string { return string(e) }

func errInvalid(msg string) error { return invalidError(msg) }

func formatMilli(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

func formatMilliPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := formatMilli(*t)
	return &s
}
