// Package herald 公告默认 Provider(M2,契约 docs/contract/announcement.md)。
//
// 能力域 announcements(/v1/announcements/*),玩家侧只读投影:
// 列表(可见期过滤,publishedAt 倒序,limit/cursor 分页)+ 详情。
// 发布走管理面(接入方运营系统)——本包 Publish 即管理面入口,发布成功后经
// Notify 钩子广播 announcement.published(接 chirp.Hub;nil = 不推送)。
package herald

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

// DefaultName 注册名(main.go 默认路由表指向它)。
const DefaultName = "herald"

// EventPublished 推送事件 type(契约 messages.md 事件登记)。
const EventPublished = "announcement.published"

const prefixAnnouncements = "/v1/announcements/"

// Options 构造参数。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// Clock 当前时间(可见期判定与发布时间;默认 time.Now)。
	Clock func() time.Time
	// RequireAuth 认证中间件(main 注入 auth.RequireAuth(verifier));
	// nil = fail-closed(一律 401):忘注入不能变成裸奔。
	RequireAuth func(http.Handler) http.Handler
	// Notify 推送钩子(announcement.published,参数为 announcementDTO);nil = 不推送。
	Notify func(eventType string, data any)
}

// Announcement 管理面实体(运营侧)。
type Announcement struct {
	ID          string
	Title       string
	Body        string
	Severity    string
	StartAt     *time.Time
	EndAt       *time.Time
	PublishedAt time.Time
	// Filters 定向过滤维度(v1.1):维度 → 值集合 开集 map;nil = 无过滤全量可见。
	// 服务端匹配依据 + 只读回显,SDK 不解释内容(契约 announcement.md)。
	Filters map[string][]string
	// Language 语种声明(F76,RFC 5646 标签;空 = 不限定语种全量可见);
	// GroupKey 管理面关联同内容多语条目(客户端不解释)。均元数据,不参与匹配。
	Language string
	GroupKey string
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

// AnnouncementProvider 公告只读投影。
type AnnouncementProvider struct {
	name   string
	clock  func() time.Time
	auth   func(http.Handler) http.Handler
	notify func(eventType string, data any)

	mu     sync.RWMutex
	nextID int
	items  []Announcement // 发布序(升序);读取时排序
}

// New 构造公告 Provider。
func New(opts Options) *AnnouncementProvider {
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
	return &AnnouncementProvider{
		name:   opts.Name,
		clock:  opts.Clock,
		auth:   authMW,
		notify: opts.Notify,
		nextID: 1,
	}
}

// Name 供应商标识。
func (p *AnnouncementProvider) Name() string { return p.name }

// Capability 能力域:announcements。
func (p *AnnouncementProvider) Capability() providers.Capability { return providers.CapAnnouncements }

// HealthCheck 内存存储常驻可用。
func (p *AnnouncementProvider) HealthCheck(_ context.Context) error { return nil }

// Publish 管理面发布(运营侧):校验、分配 ann_ ID、入列、广播。
// filters 为定向过滤维度(v1.1,开集 map,nil = 无过滤);条件 DSL 不进玩家契约。
// language/groupKey 为多语声明字段(F76,可空;language 建议 RFC 5646,不参与匹配)。
func (p *AnnouncementProvider) Publish(title, body, severity string, startAt, endAt *time.Time,
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
	p.items = append(p.items, a)
	p.nextID++
	p.mu.Unlock()

	if p.notify != nil {
		p.notify(EventPublished, p.dto(a))
	}
	return a, nil
}

// ServeHTTP 分发 /v1/announcements/*(只读,全部要求 Bearer)。
func (p *AnnouncementProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.auth(http.HandlerFunc(p.route)).ServeHTTP(w, r)
}

func (p *AnnouncementProvider) route(w http.ResponseWriter, r *http.Request) {
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

func (p *AnnouncementProvider) handleList(w http.ResponseWriter, r *http.Request) {
	limit, cursor, err := pageParams(r)
	if err != nil {
		p.writeInvalid(w, r, err.Error())
		return
	}
	dims := filterDims(r)
	now := p.clock()
	p.mu.RLock()
	visible := make([]Announcement, 0, len(p.items))
	for _, a := range p.items {
		if a.visible(now) && a.visibleTo(dims) {
			visible = append(visible, a)
		}
	}
	p.mu.RUnlock()

	// publishedAt 倒序;同刻按 ID 稳定排序。
	sort.Slice(visible, func(i, j int) bool {
		if !visible[i].PublishedAt.Equal(visible[j].PublishedAt) {
			return visible[i].PublishedAt.After(visible[j].PublishedAt)
		}
		return visible[i].ID > visible[j].ID
	})
	items := make([]announcementDTO, 0, limit)
	next := ""
	if cursor < len(visible) {
		end := cursor + limit
		if end > len(visible) {
			end = len(visible)
		}
		for _, a := range visible[cursor:end] {
			items = append(items, p.dto(a))
		}
		if end < len(visible) {
			next = strconv.Itoa(end)
		}
	}
	aggregation.WriteData(w, pageDTO{Items: items, NextCursor: next})
}

func (p *AnnouncementProvider) handleDetail(w http.ResponseWriter, r *http.Request, id string) {
	dims := filterDims(r)
	now := p.clock()
	p.mu.RLock()
	var found *Announcement
	for i := range p.items {
		if p.items[i].ID == id {
			found = &p.items[i]
			break
		}
	}
	p.mu.RUnlock()

	// 不存在/过期/未到 startAt/被定向过滤排除 一律同码:不泄露存在性(契约)。
	if found == nil || !found.visible(now) || !found.visibleTo(dims) {
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeAnnouncementNotFound, "announcement not found")
		return
	}
	aggregation.WriteData(w, p.dto(*found))
}

func (p *AnnouncementProvider) writeInvalid(w http.ResponseWriter, r *http.Request, msg string) {
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

func (p *AnnouncementProvider) dto(a Announcement) announcementDTO {
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
