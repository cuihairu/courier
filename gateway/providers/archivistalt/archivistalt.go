// Package archivistalt 玩家档案第二实现(批次 67,契约 docs/contract/player.md)。
//
// 与自建 archivist 同一冻结契约,但存储模型刻意不同——证明契约约束的是行为
// 而非实现:
//   - archivist(状态表):两个可变 map 就地更新,读=查表
//   - 本实现(事件日志):变更一律追加事件,读=投影重放;不存任何「当前状态」,
//     档案字段与绑定序全部由历史推导(事件溯源 vs 状态表,与 accountalt
//     「HMAC 令牌 vs 会话表」同构的第二实现先例)
//
// 一致性证明:e2e/player_conformance_test.go——两实现同一 wire 场景同断言
// (批次 18 identity / 64 realname 先例)。接入方切换 =
// {"player":{"primary":"archivist-alt"}} 一行配置。
//
// 红线同 archivist:角色数据归各游戏(本包只存账号↔角色映射);不采集
// 邮箱/手机/设备/行为字段;角色映射按 scope(game_id+env)隔离。
package archivistalt

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/scope"
)

// DefaultName 注册名;路由表 {"player":{"primary":"archivist-alt"}} 即换。
const DefaultName = "archivist-alt"

const prefixPlayer = "/v1/player/"

// 契约上限(player.md「数据模型」,与 archivist 同源)。
const (
	maxDisplayName = 30
	maxPlayerID    = 64
	maxCharacters  = 50
	maxAvatarURL   = 2048
	defaultName    = "Player"
)

// Options 构造参数(与 archivist.Options 同形状)。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// Clock 当前时间(事件时刻);默认 time.Now。
	Clock func() time.Time
	// RequireAuth 认证中间件;nil = fail-closed(一律 401)。
	RequireAuth func(http.Handler) http.Handler
}

// 事件种类(追加日志的最小完备集:开档/改档/绑角)。
type eventKind int

const (
	evOpened  eventKind = iota // 首次触达即开档(懒建,对应 archivist ensureProfile)
	evPatched                  // PATCH 档案(携带可选字段,与请求指针语义一致)
	evBound                    // 绑定角色(携带 scope 键与 playerId)
)

type event struct {
	kind        eventKind
	at          time.Time
	displayName *string // evPatched
	avatarURL   *string // evPatched
	scopeKey    string  // evBound: gameID|env
	playerID    string  // evBound
}

// Provider 玩家档案第二实现(事件日志模型)。
type Provider struct {
	name  string
	clock func() time.Time
	auth  func(http.Handler) http.Handler

	mu   sync.Mutex
	logs map[string][]event // accountID → 追加日志(只追加,不改写)
}

// New 构造玩家档案第二实现。
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
		name:  opts.Name,
		clock: opts.Clock,
		auth:  authMW,
		logs:  make(map[string][]event),
	}
}

// Name 供应商标识。
func (p *Provider) Name() string { return p.name }

// DeleteAccount 账号注销级联(OnAccountDeleted 订阅方):整条日志丢弃——
// 事件溯源模型下历史即状态,弃历史 = 清档案与全部角色绑定。
func (p *Provider) DeleteAccount(accountID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.logs, accountID)
}

// Capability 能力域:player。
func (p *Provider) Capability() providers.Capability { return providers.CapPlayer }

// HealthCheck 内存日志常驻可用。
func (p *Provider) HealthCheck(_ context.Context) error { return nil }

// ServeHTTP 分发 /v1/player/*(全部 Bearer)。
func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.auth(http.HandlerFunc(p.route)).ServeHTTP(w, r)
}

func (p *Provider) route(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefixPlayer)
	switch {
	case path == "profile" && r.Method == http.MethodGet:
		p.handleGetProfile(w, r)
	case path == "profile" && r.Method == http.MethodPatch:
		p.handlePatchProfile(w, r)
	case path == "characters" && r.Method == http.MethodGet:
		p.handleListCharacters(w, r)
	case path == "characters" && r.Method == http.MethodPost:
		p.handleBindCharacter(w, r)
	default:
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeNotFound, "no route for "+r.URL.Path)
	}
}

// touch 首次触达开档(懒建;对应状态表实现的 ensureProfile);调用方须持锁。
// now 在确需开档时才求值——与状态表实现「已存在不再走 clock」同语义。
func (p *Provider) touch(accountID string, now func() time.Time) {
	if _, ok := p.logs[accountID]; ok {
		return
	}
	p.logs[accountID] = []event{{kind: evOpened, at: now()}}
}

func (p *Provider) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.FromContext(r.Context())
	p.mu.Lock()
	p.touch(id.AccountID, p.clock)
	dn, av, createdAt, updatedAt := p.projectProfile(id.AccountID)
	p.mu.Unlock()
	aggregation.WriteData(w, profileDTO{
		DisplayName: dn,
		AvatarURL:   av,
		CreatedAt:   formatMilli(createdAt),
		UpdatedAt:   formatMilli(updatedAt),
	})
}

// projectProfile 从日志重放档案现状:字段=最后一次改写胜出,createdAt=开档
// 时刻,updatedAt=最后一个档案事件时刻;调用方须持锁。
func (p *Provider) projectProfile(accountID string) (displayName, avatarURL string, createdAt, updatedAt time.Time) {
	displayName = defaultName
	for _, e := range p.logs[accountID] {
		switch e.kind {
		case evOpened:
			createdAt = e.at
			updatedAt = e.at
		case evPatched:
			if e.displayName != nil {
				displayName = *e.displayName
			}
			if e.avatarURL != nil {
				avatarURL = *e.avatarURL
			}
			updatedAt = e.at
		}
	}
	return
}

func (p *Provider) handlePatchProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName *string `json:"displayName"`
		AvatarURL   *string `json:"avatarUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		p.writeInvalid(w, r, "body 须为 JSON")
		return
	}
	if req.DisplayName != nil {
		name := strings.TrimSpace(*req.DisplayName)
		if n := len([]rune(name)); n < 1 || n > maxDisplayName {
			p.writeInvalid(w, r, "displayName 须为 1-30 字符(修剪首尾后)")
			return
		}
		req.DisplayName = &name
	}
	if req.AvatarURL != nil && len(*req.AvatarURL) > maxAvatarURL {
		p.writeInvalid(w, r, "avatarUrl 过长")
		return
	}

	id, _ := auth.FromContext(r.Context())
	p.mu.Lock()
	p.touch(id.AccountID, p.clock) // 首改即开档(与状态表实现同语义)
	p.logs[id.AccountID] = append(p.logs[id.AccountID], event{
		kind: evPatched, at: p.clock(), displayName: req.DisplayName, avatarURL: req.AvatarURL,
	})
	dn, av, createdAt, updatedAt := p.projectProfile(id.AccountID)
	p.mu.Unlock()

	aggregation.WriteData(w, profileDTO{
		DisplayName: dn,
		AvatarURL:   av,
		CreatedAt:   formatMilli(createdAt),
		UpdatedAt:   formatMilli(updatedAt),
	})
}

func (p *Provider) handleListCharacters(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.FromContext(r.Context())
	key := characterKey(id.AccountID, r)
	p.mu.Lock()
	items := []characterDTO{}
	for _, e := range p.logs[id.AccountID] { // 追加序=boundAt 升序
		if e.kind == evBound && e.scopeKey == key {
			items = append(items, characterDTO{PlayerID: e.playerID, BoundAt: formatMilli(e.at)})
		}
	}
	p.mu.Unlock()
	aggregation.WriteData(w, pageDTO{Items: items, NextCursor: ""})
}

func (p *Provider) handleBindCharacter(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PlayerID string `json:"playerId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		p.writeInvalid(w, r, "body 须为 JSON")
		return
	}
	if n := len([]rune(req.PlayerID)); n < 1 || n > maxPlayerID {
		p.writeInvalid(w, r, "playerId 须为 1-64 字符")
		return
	}

	id, _ := auth.FromContext(r.Context())
	key := characterKey(id.AccountID, r)
	p.mu.Lock()
	// 幂等:已绑定返回既有(首个绑定事件的时刻,不是当前时刻;与状态表实现
	// 返回既有记录同语义)。
	for _, e := range p.logs[id.AccountID] {
		if e.kind == evBound && e.scopeKey == key && e.playerID == req.PlayerID {
			p.mu.Unlock()
			aggregation.WriteData(w, characterDTO{PlayerID: e.playerID, BoundAt: formatMilli(e.at)})
			return
		}
	}
	// 上限:该 scope 已绑定数(日志计数;幂等保证无重复事件,计数即绑定数)。
	n := 0
	for _, e := range p.logs[id.AccountID] {
		if e.kind == evBound && e.scopeKey == key {
			n++
		}
	}
	if n >= maxCharacters {
		p.mu.Unlock()
		p.writeInvalid(w, r, "绑定上限 50(每账号每游戏);请先解绑(v1.1 预留)")
		return
	}
	e := event{kind: evBound, at: p.clock(), scopeKey: key, playerID: req.PlayerID}
	p.logs[id.AccountID] = append(p.logs[id.AccountID], e)
	p.mu.Unlock()
	aggregation.WriteData(w, characterDTO{PlayerID: e.playerID, BoundAt: formatMilli(e.at)})
}

// characterKey 账号 + scope(game_id+env)联合键:同一账号在不同游戏各自成表。
func characterKey(accountID string, r *http.Request) string {
	if s, ok := scope.FromContext(r.Context()); ok {
		return accountID + "|" + s.GameID + "|" + s.Env
	}
	return accountID + "||"
}

func (p *Provider) writeInvalid(w http.ResponseWriter, r *http.Request, msg string) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeInvalidArgument, msg)
}

// wire DTO(契约数据模型,与 archivist 同形)。

type profileDTO struct {
	DisplayName string `json:"displayName"`
	AvatarURL   string `json:"avatarUrl"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

type characterDTO struct {
	PlayerID string `json:"playerId"`
	BoundAt  string `json:"boundAt"`
}

type pageDTO struct {
	Items      []characterDTO `json:"items"`
	NextCursor string         `json:"nextCursor"`
}

func formatMilli(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}
