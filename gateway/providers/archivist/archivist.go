// Package archivist 玩家档案默认 Provider(M3,契约 docs/contract/player.md)。
//
// 能力域 player(/v1/player/*),全部要求 Bearer:
//
//	GET   /v1/player/profile     账号档案(展示名/头像 URL;懒建,默认展示名 Player)
//	PATCH /v1/player/profile     修改展示名(响应体为准,服务端修剪)
//	GET   /v1/player/characters  本 scope 已绑定角色映射(boundAt 升序)
//	POST  /v1/player/characters  绑定角色(幂等;每账号每 scope ≤50)
//
// 红线:角色数据归各游戏(本包只存账号↔角色映射);不采集邮箱/手机/设备/行为字段。
// 角色映射按 scope(game_id+env)隔离——同一账号在不同游戏各自成表。
package archivist

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

// DefaultName 注册名(main.go 默认路由表指向它)。
const DefaultName = "archivist"

const prefixPlayer = "/v1/player/"

// 契约上限(player.md「数据模型」)。
const (
	maxDisplayName = 30
	maxPlayerID    = 64
	maxCharacters  = 50
	maxAvatarURL   = 2048
	defaultName    = "Player"
)

// Options 构造参数。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// Clock 当前时间(档案/绑定时间戳);默认 time.Now。
	Clock func() time.Time
	// RequireAuth 认证中间件(main 注入 auth.RequireAuth(verifier));
	// nil = fail-closed(一律 401):忘注入不能变成裸奔。
	RequireAuth func(http.Handler) http.Handler
}

type profile struct {
	DisplayName string
	AvatarURL   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type character struct {
	PlayerID string
	BoundAt  time.Time
}

// PlayerProvider 玩家档案与角色映射。
type PlayerProvider struct {
	name  string
	clock func() time.Time
	auth  func(http.Handler) http.Handler

	mu    sync.Mutex
	prof  map[string]*profile    // accountID → 档案(跨游戏)
	chars map[string][]character // accountID|gameID|env → 绑定序(boundAt 升序)
}

// New 构造玩家档案 Provider。
func New(opts Options) *PlayerProvider {
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
	return &PlayerProvider{
		name:  opts.Name,
		clock: opts.Clock,
		auth:  authMW,
		prof:  make(map[string]*profile),
		chars: make(map[string][]character),
	}
}

// Name 供应商标识。
func (p *PlayerProvider) Name() string { return p.name }

// Capability 能力域:player。
func (p *PlayerProvider) Capability() providers.Capability { return providers.CapPlayer }

// HealthCheck 内存存储常驻可用。
func (p *PlayerProvider) HealthCheck(_ context.Context) error { return nil }

// ServeHTTP 分发 /v1/player/*(全部 Bearer)。
func (p *PlayerProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.auth(http.HandlerFunc(p.route)).ServeHTTP(w, r)
}

func (p *PlayerProvider) route(w http.ResponseWriter, r *http.Request) {
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

func (p *PlayerProvider) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.FromContext(r.Context())
	p.mu.Lock()
	prof := p.ensureProfile(id.AccountID) // 懒建:默认展示名,首次 GET 即建档
	p.mu.Unlock()
	aggregation.WriteData(w, profileDTO{
		DisplayName: prof.DisplayName,
		AvatarURL:   prof.AvatarURL,
		CreatedAt:   formatMilli(prof.CreatedAt),
		UpdatedAt:   formatMilli(prof.UpdatedAt),
	})
}

func (p *PlayerProvider) handlePatchProfile(w http.ResponseWriter, r *http.Request) {
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
	prof := p.ensureProfile(id.AccountID)
	if req.DisplayName != nil {
		prof.DisplayName = *req.DisplayName
	}
	if req.AvatarURL != nil {
		prof.AvatarURL = *req.AvatarURL
	}
	prof.UpdatedAt = p.clock()
	snapshot := *prof
	p.mu.Unlock()

	aggregation.WriteData(w, profileDTO{
		DisplayName: snapshot.DisplayName,
		AvatarURL:   snapshot.AvatarURL,
		CreatedAt:   formatMilli(snapshot.CreatedAt),
		UpdatedAt:   formatMilli(snapshot.UpdatedAt),
	})
}

func (p *PlayerProvider) handleListCharacters(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.FromContext(r.Context())
	key := characterKey(id.AccountID, r)
	p.mu.Lock()
	list := p.chars[key]
	items := make([]characterDTO, 0, len(list))
	for _, c := range list { // 绑定序即 boundAt 升序(只追加)
		items = append(items, characterDTO{PlayerID: c.PlayerID, BoundAt: formatMilli(c.BoundAt)})
	}
	p.mu.Unlock()
	aggregation.WriteData(w, pageDTO{Items: items, NextCursor: ""})
}

func (p *PlayerProvider) handleBindCharacter(w http.ResponseWriter, r *http.Request) {
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
	for _, c := range p.chars[key] { // 幂等:已绑定返回既有(200,不是错误)
		if c.PlayerID == req.PlayerID {
			p.mu.Unlock()
			aggregation.WriteData(w, characterDTO{PlayerID: c.PlayerID, BoundAt: formatMilli(c.BoundAt)})
			return
		}
	}
	if len(p.chars[key]) >= maxCharacters {
		p.mu.Unlock()
		p.writeInvalid(w, r, "绑定上限 50(每账号每游戏);请先解绑(v1.1 预留)")
		return
	}
	c := character{PlayerID: req.PlayerID, BoundAt: p.clock()}
	p.chars[key] = append(p.chars[key], c)
	p.mu.Unlock()
	aggregation.WriteData(w, characterDTO{PlayerID: c.PlayerID, BoundAt: formatMilli(c.BoundAt)})
}

// ensureProfile 调用方须持锁。
func (p *PlayerProvider) ensureProfile(accountID string) *profile {
	if prof, ok := p.prof[accountID]; ok {
		return prof
	}
	now := p.clock()
	prof := &profile{DisplayName: defaultName, CreatedAt: now, UpdatedAt: now}
	p.prof[accountID] = prof
	return prof
}

// characterKey 账号 + scope(game_id+env)联合键:同一账号在不同游戏各自成表。
func characterKey(accountID string, r *http.Request) string {
	if s, ok := scope.FromContext(r.Context()); ok {
		return accountID + "|" + s.GameID + "|" + s.Env
	}
	return accountID + "||"
}

func (p *PlayerProvider) writeInvalid(w http.ResponseWriter, r *http.Request, msg string) {
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeInvalidArgument, msg)
}

// wire DTO(契约数据模型,camelCase;缺省边界输出 null)。

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
