// Package scribealt 远程配置第二实现(批次 71,契约 docs/contract/config.md、app.md)。
//
// 与默认 scribe 同一冻结契约,但条件求值模型刻意不同——证明契约约束的是
// 行为而非实现:
//   - scribe(读时逐条目求值):每次投影迭代每个条目的条件切片,逐条件 AND
//     短路,缺省维度跳过
//   - 本实现(写时维度索引 + 读时按维度分阶段过滤):Publish 时把条件切片
//     转为维度→允许值集合的索引;投影时迭代请求提供的维度,按维度过滤
//     条目,而非逐条目求值
//
// 两模型等价:都只检查「条目条件维度 ∩ 请求提供维度」的非空值对——scribe
// 迭代条目条件跳过请求未提供的维度,本实现迭代请求维度跳过条目未声明的
// 维度,同一检查集合、同一保留/丢弃结果。
//
// 发布校验/版本门槛/灰度分桶/品牌/维护/环境逐字同源(契约行为);
// 一致性证明:e2e/config_conformance_test.go——两实现同一 wire 场景同断言
// (批次 18/64/67/68/69/70 先例)。接入方切换 =
// {"app":{"primary":"scribe-alt"}} 一行配置。
package scribealt

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
	"github.com/cuihairu/courier/gateway/providers"
	"github.com/cuihairu/courier/gateway/scope"
)

// DefaultName 注册名;路由表 {"app":{"primary":"scribe-alt"}} 即换。
const DefaultName = "scribe-alt"

// EventConfigUpdated 推送事件 type(契约 config.md,与 scribe 同值)。
const EventConfigUpdated = "config.updated"

// EventBrandingUpdated 推送事件 type(契约 branding.md,与 scribe 同值)。
const EventBrandingUpdated = "branding.updated"

const prefixApp = "/v1/app/"

// 契约上限(config.md「能力与范围」,与 scribe 同值)。
const (
	maxKeys       = 200
	maxKeyLen     = 128
	maxValueBytes = 32 * 1024
)

var keyPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// Options 构造参数(与 scribe.Options 同形状)。
type Options struct {
	// Name 供应商标识(默认 DefaultName)。
	Name string
	// RequireAuth 认证中间件;nil = fail-closed(config 路由一律 401)。
	RequireAuth func(http.Handler) http.Handler
	// Notify 推送钩子(config.updated / branding.updated);nil = 不推送。
	Notify func(eventType string, data any)
}

// Dimension 条件维度(契约:玩家侧请求参数只此三种)。
type Dimension string

const (
	DimPlatform   Dimension = "platform"
	DimAppVersion Dimension = "appVersion"
	DimRegion     Dimension = "region"
)

var knownDimensions = map[Dimension]bool{DimPlatform: true, DimAppVersion: true, DimRegion: true}

// Condition 单维度条件(与 scribe 同形:管理面实体)。
type Condition struct {
	Dimension Dimension
	Values    []string
}

// Entry 一条配置(管理面实体,与 scribe 同形);写时派生维度索引。
type Entry struct {
	Key        string
	Value      any
	Conditions []Condition
	Percent    int // 0(缺省)与 100 = 全量;1-99 = hash(accountId|key) 稳定分桶

	raw   json.RawMessage        // 发布时序列化物化(投影直接回写)
	byDim map[Dimension][]string // 写时派生:条件维度索引(异构面)
}

// VersionInfo 版本门槛(管理面设置,与 scribe 同形)。
type VersionInfo struct {
	LatestVersion string
	MinVersion    string
	UpdateURL     string
}

// MaintenanceState 维护开关(管理面设置,与 scribe 同形)。
type MaintenanceState struct {
	InMaintenance       bool
	EstimatedRecoveryAt *time.Time
	Message             string
}

// AppProvider 远程配置/应用状态第二实现(写时维度索引模型)。
type AppProvider struct {
	name   string
	auth   func(http.Handler) http.Handler
	notify func(eventType string, data any)

	mu    sync.RWMutex
	ver   int64
	items []Entry

	version VersionInfo
	maint   MaintenanceState

	brandingVer int64
	branding    map[string]json.RawMessage
}

// New 构造 App Provider 第二实现。
func New(opts Options) *AppProvider {
	if opts.Name == "" {
		opts.Name = DefaultName
	}
	authMW := func(next http.Handler) http.Handler {
		// fail-closed:未注入认证中间件时拒绝 config 路由。
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			aggregation.WriteError(w, middleware.TraceID(r.Context()),
				aggregation.CodeUnauthenticated, "not authenticated")
		})
	}
	if opts.RequireAuth != nil {
		authMW = opts.RequireAuth
	}
	return &AppProvider{name: opts.Name, auth: authMW, notify: opts.Notify}
}

// Name 供应商标识。
func (p *AppProvider) Name() string { return p.name }

// Capability 能力域:app。
func (p *AppProvider) Capability() providers.Capability { return providers.CapApp }

// HealthCheck 内存存储常驻可用。
func (p *AppProvider) HealthCheck(_ context.Context) error { return nil }

// Publish 管理面发布:全量替换配置集,产生新 configVersion;写时构建维度索引
// (异构面:sage 在每次投影时逐条目求值条件,本实现只做一次)。
func (p *AppProvider) Publish(entries []Entry) (int64, error) {
	if err := validateEntries(entries); err != nil {
		return 0, err
	}
	// 序列化物化与限额校验在锁外做(纯计算),锁内只替换切片。
	stored := make([]Entry, len(entries))
	for i, e := range entries {
		raw, err := json.Marshal(e.Value)
		if err != nil {
			return 0, errInvalid(fmt.Sprintf("entries[%d].Value 非可序列化 JSON:%v", i, err))
		}
		if len(raw) > maxValueBytes {
			return 0, errInvalid(fmt.Sprintf("entries[%d].Value 超 32 KiB 上限", i))
		}
		e.raw = raw
		// 写时维度索引:条件切片 → 维度→允许值集合(读时迭代请求维度查表)。
		e.byDim = buildDimIndex(e.Conditions)
		stored[i] = e
	}
	p.mu.Lock()
	p.ver++
	p.items = stored
	v := p.ver
	p.mu.Unlock()

	if p.notify != nil {
		p.notify(EventConfigUpdated, map[string]any{"configVersion": v})
	}
	return v, nil
}

// ConfigVersion 当前全局版本(测试与观测)。
func (p *AppProvider) ConfigVersion() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.ver
}

// SetVersion 管理面设置版本门槛。
func (p *AppProvider) SetVersion(v VersionInfo) {
	p.mu.Lock()
	p.version = v
	p.mu.Unlock()
}

// SetMaintenance 管理面设置维护开关;开启后经 middleware.Maintenance 拦新会话。
func (p *AppProvider) SetMaintenance(m MaintenanceState) {
	p.mu.Lock()
	p.maint = m
	p.mu.Unlock()
}

// InMaintenance 维护门读数(middleware.Maintenance 的 enabled 回调)。
func (p *AppProvider) InMaintenance() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.maint.InMaintenance
}

// ServeHTTP 分发 /v1/app/*(config 要求 Bearer;version/maintenance/
// environment 匿名可)。
func (p *AppProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefixApp)
	switch {
	case path == "config":
		if r.Method == http.MethodGet {
			p.auth(http.HandlerFunc(p.handleConfig)).ServeHTTP(w, r)
			return
		}
	case path == "version" && r.Method == http.MethodGet:
		p.handleVersion(w, r)
		return
	case path == "maintenance" && r.Method == http.MethodGet:
		p.handleMaintenance(w, r)
		return
	case path == "branding" && r.Method == http.MethodGet:
		p.handleBranding(w, r)
		return
	case path == "environment" && r.Method == http.MethodGet:
		p.handleEnvironment(w, r)
		return
	}
	aggregation.WriteError(w, middleware.TraceID(r.Context()),
		aggregation.CodeNotFound, "no route for "+r.URL.Path)
}

// handleConfig 投影:按维度分阶段过滤(异构面)+ 灰度(账号稳定分桶)。
// 逐维度迭代请求提供的维度,保留「条目无此维度条件」或「条件命中」的条目;
// 等价于 scribe 的逐条目条件 AND 短路(同一检查集合)。
func (p *AppProvider) handleConfig(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeUnauthenticated, "not authenticated")
		return
	}
	q := r.URL.Query()
	dims := map[Dimension]string{}
	for _, d := range []Dimension{DimPlatform, DimAppVersion, DimRegion} {
		if v := q.Get(string(d)); v != "" {
			dims[d] = v
		}
	}

	p.mu.RLock()
	v := p.ver
	items := make(map[string]json.RawMessage, len(p.items))
	for _, e := range p.items {
		// 按维度分阶段过滤:迭代请求维度,而非条目条件。
		if dimsMatch(e.byDim, dims) && inGrayBucket(id.AccountID, e.Key, e.Percent) {
			items[e.Key] = e.raw
		}
	}
	p.mu.RUnlock()

	aggregation.WriteData(w, configDTO{ConfigVersion: v, Items: items})
}

// dimsMatch 按维度分阶段判定:对请求提供的每个维度,条目无此维度条件 →
// 保留;有条目条件 → 值须 ∈ 允许集合。与 scribe.matchEntry 逐条目 AND 短路
// 等价(同一检查集合:条目条件维度 ∩ 请求提供维度)。
func dimsMatch(byDim map[Dimension][]string, dims map[Dimension]string) bool {
	for dim, val := range dims {
		allowed, has := byDim[dim]
		if !has {
			continue // 条目无此维度条件 → 该维度不过滤(缺省维度不参与过滤)
		}
		if !contains(allowed, val) {
			return false
		}
	}
	return true
}

// buildDimIndex 写时派生:条件切片 → 维度→允许值集合索引。
func buildDimIndex(conditions []Condition) map[Dimension][]string {
	if len(conditions) == 0 {
		return nil
	}
	idx := make(map[Dimension][]string, len(conditions))
	for _, c := range conditions {
		idx[c.Dimension] = c.Values
	}
	return idx
}

// handleVersion 版本门槛判定:appVersion 缺省 = 不判定(forceUpdate false)。
func (p *AppProvider) handleVersion(w http.ResponseWriter, r *http.Request) {
	force := false
	if av := r.URL.Query().Get("appVersion"); av != "" {
		p.mu.RLock()
		min := p.version.MinVersion
		p.mu.RUnlock()
		if min != "" {
			c, err := compareVersion(av, min)
			if err != nil {
				aggregation.WriteError(w, middleware.TraceID(r.Context()),
					aggregation.CodeInvalidArgument, "appVersion 非法(点分数字)")
				return
			}
			force = c < 0
		}
	}
	p.mu.RLock()
	dto := versionDTO{
		LatestVersion: p.version.LatestVersion,
		MinVersion:    p.version.MinVersion,
		UpdateURL:     p.version.UpdateURL,
		ForceUpdate:   force,
	}
	p.mu.RUnlock()
	aggregation.WriteData(w, dto)
}

// handleMaintenance 维护状态(维护中仍可达:维护门白名单放行本域)。
func (p *AppProvider) handleMaintenance(w http.ResponseWriter, _ *http.Request) {
	p.mu.RLock()
	dto := maintenanceDTO{
		InMaintenance:       p.maint.InMaintenance,
		EstimatedRecoveryAt: formatMilliPtr(p.maint.EstimatedRecoveryAt),
	}
	// 局部拷贝再取址:不让字段指针逃逸出读锁(竞患)。
	if msg := p.maint.Message; msg != "" {
		dto.Message = &msg
	}
	p.mu.RUnlock()
	aggregation.WriteData(w, dto)
}

// SetBranding 管理面设置品牌物料(与 scribe 同源)。
func (p *AppProvider) SetBranding(data []byte) (int64, error) {
	fields := make(map[string]json.RawMessage)
	if err := json.Unmarshal(data, &fields); err != nil {
		return 0, errInvalid("branding 须为 JSON 对象:" + err.Error())
	}
	if fields == nil {
		return 0, errInvalid("branding 须为 JSON 对象")
	}
	if _, reserved := fields["version"]; reserved {
		return 0, errInvalid(`branding 业务字段不得占用 "version" 键`)
	}
	p.mu.Lock()
	p.brandingVer++
	p.branding = fields
	v := p.brandingVer
	p.mu.Unlock()

	if p.notify != nil {
		p.notify(EventBrandingUpdated, map[string]any{"version": v})
	}
	return v, nil
}

// handleBranding 品牌投影(匿名可:登录页就要显示品牌)。
func (p *AppProvider) handleBranding(w http.ResponseWriter, _ *http.Request) {
	p.mu.RLock()
	v := p.brandingVer
	out := make(map[string]json.RawMessage, len(p.branding)+1)
	for k, raw := range p.branding {
		out[k] = raw
	}
	p.mu.RUnlock()
	out["version"] = json.RawMessage(strconv.FormatInt(v, 10))
	aggregation.WriteData(w, out)
}

// handleEnvironment scope 回显(初始化校验;scope 由链上中间件注入)。
func (p *AppProvider) handleEnvironment(w http.ResponseWriter, r *http.Request) {
	s, ok := scope.FromContext(r.Context())
	if !ok {
		aggregation.WriteError(w, middleware.TraceID(r.Context()),
			aggregation.CodeInvalidArgument, "scope 缺失")
		return
	}
	aggregation.WriteData(w, environmentDTO{GameID: s.GameID, Env: s.Env})
}

// inGrayBucket 灰度分桶(与 scribe 同源):0 与 100 全量;1-99 按
// fnv64(accountId|key) % 100 < percent。只依赖账号与键,不依赖版本。
func inGrayBucket(accountID, key string, percent int) bool {
	if percent <= 0 || percent >= 100 {
		return true
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(accountID + "|" + key))
	return h.Sum64()%100 < uint64(percent)
}

func contains(values []string, v string) bool {
	for _, s := range values {
		if s == v {
			return true
		}
	}
	return false
}

// validateEntries 管理面发布校验(与 scribe 同源:契约上限与防呆)。
func validateEntries(entries []Entry) error {
	if len(entries) > maxKeys {
		return errInvalid(fmt.Sprintf("配置集 ≤%d 键", maxKeys))
	}
	seen := make(map[string]bool, len(entries))
	for i, e := range entries {
		if n := len(e.Key); n < 1 || n > maxKeyLen || !keyPattern.MatchString(e.Key) {
			return errInvalid(fmt.Sprintf("entries[%d].key 须为 1-%d 字符 [a-zA-Z0-9_.-]", i, maxKeyLen))
		}
		if seen[e.Key] {
			return errInvalid(fmt.Sprintf("重复键 %q", e.Key))
		}
		seen[e.Key] = true
		if e.Percent < 0 || e.Percent > 100 {
			return errInvalid(fmt.Sprintf("entries[%d].Percent 须为 0-100", i))
		}
		for _, c := range e.Conditions {
			if !knownDimensions[c.Dimension] {
				return errInvalid(fmt.Sprintf("entries[%d] 未知条件维度 %q(玩家侧仅 platform/appVersion/region)", i, c.Dimension))
			}
			if len(c.Values) == 0 {
				return errInvalid(fmt.Sprintf("entries[%d] 条件 %s 缺 Values", i, c.Dimension))
			}
		}
	}
	return nil
}

// compareVersion 点分数字版本比较(与 scribe 同源:a<b → -1,a=b → 0,a>b → 1)。
func compareVersion(a, b string) (int, error) {
	as, err := versionSegments(a)
	if err != nil {
		return 0, err
	}
	bs, err := versionSegments(b)
	if err != nil {
		return 0, err
	}
	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		x, y := 0, 0
		if i < len(as) {
			x = as[i]
		}
		if i < len(bs) {
			y = bs[i]
		}
		if x != y {
			if x < y {
				return -1, nil
			}
			return 1, nil
		}
	}
	return 0, nil
}

func versionSegments(v string) ([]int, error) {
	if v == "" {
		return nil, errInvalid("版本号为空")
	}
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, s := range parts {
		n := 0
		if s == "" {
			return nil, errInvalid(fmt.Sprintf("版本段 %q 非法", s))
		}
		for _, ch := range s {
			if ch < '0' || ch > '9' {
				return nil, errInvalid(fmt.Sprintf("版本段 %q 非数字", s))
			}
			n = n*10 + int(ch-'0')
		}
		out[i] = n
	}
	return out, nil
}

// errInvalid 参数类错误(信封 COMMON_INVALID_ARGUMENT)。
type invalidError string

func (e invalidError) Error() string { return string(e) }

func errInvalid(msg string) error { return invalidError(msg) }

// wire DTO(契约数据模型,camelCase;与 scribe 同形)。

type configDTO struct {
	ConfigVersion int64                      `json:"configVersion"`
	Items         map[string]json.RawMessage `json:"items"`
}

type versionDTO struct {
	LatestVersion string `json:"latestVersion"`
	MinVersion    string `json:"minVersion"`
	UpdateURL     string `json:"updateUrl,omitempty"`
	ForceUpdate   bool   `json:"forceUpdate"`
}

type maintenanceDTO struct {
	InMaintenance       bool    `json:"inMaintenance"`
	EstimatedRecoveryAt *string `json:"estimatedRecoveryAt,omitempty"`
	Message             *string `json:"message,omitempty"`
}

type environmentDTO struct {
	GameID string `json:"gameId"`
	Env    string `json:"env"`
}

func formatMilliPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	return &s
}
