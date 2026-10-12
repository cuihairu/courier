package account

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/providers/oauth"
	"github.com/cuihairu/courier/gateway/session"
)

// 存储层错误(错误码由上层映射,见 docs/contract/auth.md)。
var (
	ErrEmailTaken         = errors.New("account: 邮箱已被注册")
	ErrDeviceLimit        = errors.New("account: 设备数超限")
	ErrNotFound           = errors.New("account: 不存在")
	ErrAlreadyBound       = errors.New("account: 账号已绑定邮箱")
	ErrDisabled           = errors.New("account: 账号禁用")
	ErrDeleteTokenInvalid = errors.New("account: 撤销 token 无效或过期")
)

// Options 存储与会话参数(契约 auth.md:TTL/上限接入方可配,契约不冻结数值)。
type Options struct {
	AccessTTL     time.Duration // 默认 15m
	RefreshTTL    time.Duration // 默认 30d
	MaxDevices    int           // 默认 5
	Iterations    int           // PBKDF2 迭代,默认 210000(测试调低)
	RatePerMinute int           // 敏感端点每 IP 限流,默认 10
	RateBurst     int           // 默认 10
	DeleteGrace   time.Duration // 注销宽限期(v1.1),负值=未配置走默认 14d,0 = EMAIL 也立即删;上限 30d(管理面钳制)
	OAuthDisabled bool          // oauth 域关闭(v1.1,oauth.md「可换可关」);true → oauth 端点 501
	Clock         func() time.Time
}

// fillDefaults 补默认值。
func (o *Options) fillDefaults() {
	if o.AccessTTL <= 0 {
		o.AccessTTL = 15 * time.Minute
	}
	if o.RefreshTTL <= 0 {
		o.RefreshTTL = 30 * 24 * time.Hour
	}
	if o.MaxDevices <= 0 {
		o.MaxDevices = 5
	}
	if o.Iterations <= 0 {
		o.Iterations = DefaultIterations
	}
	if o.RatePerMinute <= 0 {
		o.RatePerMinute = 10
	}
	if o.RateBurst <= 0 {
		o.RateBurst = 10
	}
	if o.DeleteGrace < 0 {
		// 负值 = 未配置走契约缺省;0 是合法配置(EMAIL 也立即删,契约 0–30d)。
		o.DeleteGrace = DefaultDeleteGrace
	}
	if o.DeleteGrace > 30*24*time.Hour {
		o.DeleteGrace = 30 * 24 * time.Hour // 契约上限,管理面配置钳制
	}
	if o.Clock == nil {
		o.Clock = time.Now
	}
}

// 账号/会话状态枚举(契约 auth.md「数据模型」;后两值 v1.1 注销;
// OAUTH 为 v1.1 加法,随 oauth.md v1 冻结——纯第三方登录创建,无凭证行)。
const (
	AccountEmail       = "EMAIL"
	AccountGuest       = "GUEST"
	AccountOauth       = "OAUTH"
	StatusActive       = "ACTIVE"
	StatusDisabled     = "DISABLED"
	StatusPendingDel   = "PENDING_DELETE"
	StatusDeleted      = "DELETED"
	DefaultDeleteGrace = 14 * 24 * time.Hour
)

// 四表行结构,字段口径与 docs/contract/auth.md「数据模型」一致。
type accountRow struct {
	id          string    // acc_<uuidv7>
	typ         string    // EMAIL | GUEST
	email       string    // GUEST 为空
	status      string    // ACTIVE | DISABLED | PENDING_DELETE | DELETED
	deleteAfter time.Time // PENDING_DELETE 宽限期满时刻(v1.1)
	createdAt   time.Time
	updatedAt   time.Time
}

type credentialRow struct {
	accountID  string
	hash, salt []byte
	algo       string
	iterations int
	updatedAt  time.Time
}

type sessionRow struct {
	id               string // ses_<uuidv7>
	accountID        string
	gameID, env      string
	deviceID         string // 可空
	accessHash       string // sha256 hex,不存原文
	refreshHash      string
	prevRefreshHash  string // 重放检测(契约 auth.md「Token 模型」)
	accessExpiresAt  time.Time
	refreshExpiresAt time.Time
	createdAt        time.Time
	revokedAt        *time.Time
}

type deviceRow struct {
	id         string // dev_<uuidv7>
	accountID  string
	deviceID   string
	platform   string
	createdAt  time.Time
	lastSeenAt time.Time
}

// pendingDeleteRow 注销宽限行(v1.1):tokenHash 只存哈希,一次性。
type pendingDeleteRow struct {
	accountID string
	tokenHash string
	expiresAt time.Time
}

// Store 内存存储,四表结构与 auth.md 附录 DDL 同构(外加 v1.1 oauth 域
// account_identities 映射表);粗粒度互斥锁。
// 持久化(SQLite/Postgres)随 M1 后续工程替换,接口形状不变。
type Store struct {
	mu   sync.Mutex
	opts Options

	accounts       map[string]*accountRow
	credentials    map[string]*credentialRow
	sessions       map[string]*sessionRow
	devices        map[string]*deviceRow
	pendingDeletes map[string]*pendingDeleteRow // tokenHash → 宽限行(v1.1)
	identities     *oauth.Identities            // account_identities(v1.1,oauth.md)

	emailIndex     map[string]string // email → accountID
	guestDevIndex  map[string]string // deviceId → guest accountID(游客幂等)
	deviceKeys     map[string]string // "accountID|deviceId" → 行 ID
	accessIndex    map[string]string // sha256(access) → sessionID
	refreshIndex   map[string]string // sha256(current refresh) → sessionID
	prevRefreshIdx map[string]string // sha256(previous refresh) → sessionID(重放检测)
}

// NewStore 内存存储。
func NewStore(opts Options) *Store {
	opts.fillDefaults()
	s := &Store{
		opts:           opts,
		accounts:       make(map[string]*accountRow),
		credentials:    make(map[string]*credentialRow),
		sessions:       make(map[string]*sessionRow),
		devices:        make(map[string]*deviceRow),
		pendingDeletes: make(map[string]*pendingDeleteRow),
		emailIndex:     make(map[string]string),
		guestDevIndex:  make(map[string]string),
		deviceKeys:     make(map[string]string),
		accessIndex:    make(map[string]string),
		refreshIndex:   make(map[string]string),
		prevRefreshIdx: make(map[string]string),
	}
	s.identities = oauth.NewIdentities(s.now)
	return s
}

// Options 只读访问。
func (s *Store) Options() Options { return s.opts }

func (s *Store) now() time.Time { return s.opts.Clock() }

func (s *Store) newID(prefix string, t time.Time) string {
	return prefix + newUUIDv7(t)
}

// --- accounts ---

// CreateEmailAccount 注册 EMAIL 账号;email 已存在 → ErrEmailTaken。
func (s *Store) CreateEmailAccount(email, password string) (accountRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.now()
	if _, taken := s.emailIndex[email]; taken {
		return accountRow{}, ErrEmailTaken
	}
	hash, salt, err := hashPassword(password, s.opts.Iterations)
	if err != nil {
		return accountRow{}, err
	}
	id := s.newID("acc_", t)
	s.accounts[id] = &accountRow{id: id, typ: AccountEmail, email: email, status: StatusActive, createdAt: t, updatedAt: t}
	s.emailIndex[email] = id
	s.credentials[id] = &credentialRow{
		accountID: id, hash: hash, salt: salt,
		algo: AlgoPBKDF2SHA256, iterations: s.opts.Iterations, updatedAt: t,
	}
	return *s.accounts[id], nil
}

// FindGuestByDevice 游客幂等:按设备找 GUEST 账号(auth.md「数据模型:devices」)。
func (s *Store) FindGuestByDevice(deviceID string) (accountRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.guestDevIndex[deviceID]
	if !ok {
		return accountRow{}, false
	}
	acc, ok := s.accounts[id]
	if !ok || acc.typ != AccountGuest {
		return accountRow{}, false
	}
	return *acc, true
}

// CreateGuestAccount 创建游客账号并绑定首台设备(首绑不受上限约束)。
func (s *Store) CreateGuestAccount(deviceID, platform string) (accountRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.now()
	id := s.newID("acc_", t)
	acc := &accountRow{id: id, typ: AccountGuest, status: StatusActive, createdAt: t, updatedAt: t}
	s.accounts[id] = acc
	if _, err := s.bindDeviceLocked(acc, deviceID, platform, t); err != nil {
		delete(s.accounts, id)
		return accountRow{}, err
	}
	return *acc, nil
}

// GetAccount 按 ID 查询。
func (s *Store) GetAccount(id string) (accountRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.accounts[id]
	if !ok {
		return accountRow{}, false
	}
	return *acc, true
}

// BindEmail 游客转正:写 email + 凭证;非游客 → ErrAlreadyBound;被占 → ErrEmailTaken。
func (s *Store) BindEmail(accountID, email, password string) (accountRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.now()
	acc, ok := s.accounts[accountID]
	if !ok {
		return accountRow{}, ErrNotFound
	}
	if acc.typ != AccountGuest {
		return accountRow{}, ErrAlreadyBound
	}
	if other, taken := s.emailIndex[email]; taken && other != accountID {
		return accountRow{}, ErrEmailTaken
	}
	hash, salt, err := hashPassword(password, s.opts.Iterations)
	if err != nil {
		return accountRow{}, err
	}
	acc.typ = AccountEmail
	acc.email = email
	acc.updatedAt = t
	s.emailIndex[email] = accountID
	s.credentials[accountID] = &credentialRow{
		accountID: accountID, hash: hash, salt: salt,
		algo: AlgoPBKDF2SHA256, iterations: s.opts.Iterations, updatedAt: t,
	}
	return *acc, nil
}

// VerifyEmailPassword 登录核验;账号或凭证缺失与密码错误同返回 false(防枚举)。
func (s *Store) VerifyEmailPassword(email, password string) (accountRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.emailIndex[email]
	if !ok {
		return accountRow{}, false
	}
	cred, ok := s.credentials[id]
	if !ok {
		return accountRow{}, false
	}
	if !verifyPassword(password, cred.salt, cred.hash, cred.iterations) {
		return accountRow{}, false
	}
	return *s.accounts[id], true
}

// --- devices ---

func deviceKey(accountID, deviceID string) string { return accountID + "|" + deviceID }

func (s *Store) countDevicesLocked(accountID string) int {
	n := 0
	for _, d := range s.devices {
		if d.accountID == accountID {
			n++
		}
	}
	return n
}

// bindDeviceLocked 已绑定则触碰 lastSeenAt;新设备受上限约束。
func (s *Store) bindDeviceLocked(acc *accountRow, deviceID, platform string, t time.Time) (deviceRow, error) {
	key := deviceKey(acc.id, deviceID)
	if rowID, exists := s.deviceKeys[key]; exists {
		d := s.devices[rowID]
		d.platform = platform
		d.lastSeenAt = t
		return *d, nil
	}
	if s.countDevicesLocked(acc.id) >= s.opts.MaxDevices {
		return deviceRow{}, ErrDeviceLimit
	}
	d := &deviceRow{
		id: s.newID("dev_", t), accountID: acc.id,
		deviceID: deviceID, platform: platform,
		createdAt: t, lastSeenAt: t,
	}
	s.devices[d.id] = d
	s.deviceKeys[key] = d.id
	if acc.typ == AccountGuest {
		s.guestDevIndex[deviceID] = acc.id
	}
	return *d, nil
}

// EnsureBindDevice 登录时绑定设备。
func (s *Store) EnsureBindDevice(accountID, deviceID, platform string) (deviceRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.accounts[accountID]
	if !ok {
		return deviceRow{}, ErrNotFound
	}
	return s.bindDeviceLocked(acc, deviceID, platform, s.now())
}

// ListDevices 设备列表(auth.md GET /devices)。
func (s *Store) ListDevices(accountID string) []deviceRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]deviceRow, 0)
	for _, d := range s.devices {
		if d.accountID == accountID {
			out = append(out, *d)
		}
	}
	return out
}

// UnbindDevice 解绑并吊销该设备全部会话;设备不存在返回 false。
// 游客账号解绑后,同设备的下次游客登录会创建新游客账号(幂等键随解绑失效)。
func (s *Store) UnbindDevice(accountID, deviceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := deviceKey(accountID, deviceID)
	rowID, ok := s.deviceKeys[key]
	if !ok {
		return false
	}
	delete(s.devices, rowID)
	delete(s.deviceKeys, key)
	if s.guestDevIndex[deviceID] == accountID {
		delete(s.guestDevIndex, deviceID)
	}
	t := s.now()
	for _, sess := range s.sessions {
		if sess.accountID == accountID && sess.deviceID == deviceID && sess.revokedAt == nil {
			sess.revokedAt = &t
		}
	}
	return true
}

// --- sessions ---

// sha256Hex token 只存哈希(契约:不存原文)。
func sha256Hex(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

func genToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateSession 签发会话,返回明文 access/refresh(仅此一次可见);账号禁用 → ErrDisabled。
func (s *Store) CreateSession(accountID, gameID, env, deviceID string) (sess sessionRow, access, refresh string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.accounts[accountID]
	if !ok {
		return sessionRow{}, "", "", ErrNotFound
	}
	if acc.status != StatusActive {
		return sessionRow{}, "", "", ErrDisabled
	}
	t := s.now()
	access, err = genToken()
	if err != nil {
		return sessionRow{}, "", "", err
	}
	refresh, err = genToken()
	if err != nil {
		return sessionRow{}, "", "", err
	}
	row := &sessionRow{
		id:               s.newID("ses_", t),
		accountID:        accountID,
		gameID:           gameID,
		env:              env,
		deviceID:         deviceID,
		accessHash:       sha256Hex(access),
		refreshHash:      sha256Hex(refresh),
		accessExpiresAt:  t.Add(s.opts.AccessTTL),
		refreshExpiresAt: t.Add(s.opts.RefreshTTL),
		createdAt:        t,
	}
	s.sessions[row.id] = row
	s.accessIndex[row.accessHash] = row.id
	s.refreshIndex[row.refreshHash] = row.id
	return *row, access, refresh, nil
}

// RefreshGeneration refresh token 所属代(重放检测)。
type RefreshGeneration int

const (
	RefreshNone RefreshGeneration = iota
	RefreshCurrent
	RefreshPrevious
)

// LookupRefresh 定位 refresh token → (会话 ID, 所属代)。
func (s *Store) LookupRefresh(refresh string) (string, RefreshGeneration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := sha256Hex(refresh)
	if id, ok := s.refreshIndex[h]; ok {
		return id, RefreshCurrent
	}
	if id, ok := s.prevRefreshIdx[h]; ok {
		return id, RefreshPrevious
	}
	return "", RefreshNone
}

// RotateSession 轮换:旧 access 立即失效,旧 refresh 记入 previous;
// refresh 已过期 → auth.ErrTokenExpired(客户端应重登)。
func (s *Store) RotateSession(sessionID string) (sess sessionRow, access, refresh string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.now()
	row, ok := s.sessions[sessionID]
	if !ok {
		return sessionRow{}, "", "", ErrNotFound
	}
	if row.revokedAt != nil {
		return sessionRow{}, "", "", auth.ErrTokenRevoked
	}
	if t.After(row.refreshExpiresAt) {
		return sessionRow{}, "", "", auth.ErrTokenExpired
	}
	access, err = genToken()
	if err != nil {
		return sessionRow{}, "", "", err
	}
	refresh, err = genToken()
	if err != nil {
		return sessionRow{}, "", "", err
	}
	if row.prevRefreshHash != "" {
		delete(s.prevRefreshIdx, row.prevRefreshHash) // 只保留上一代
	}
	delete(s.refreshIndex, row.refreshHash)
	delete(s.accessIndex, row.accessHash) // 旧 access 立即失效(auth.md「Token 模型」)
	row.prevRefreshHash = row.refreshHash
	s.prevRefreshIdx[row.prevRefreshHash] = row.id

	row.accessHash = sha256Hex(access)
	row.refreshHash = sha256Hex(refresh)
	s.accessIndex[row.accessHash] = row.id
	s.refreshIndex[row.refreshHash] = row.id
	row.accessExpiresAt = t.Add(s.opts.AccessTTL)
	row.refreshExpiresAt = t.Add(s.opts.RefreshTTL)
	return *row, access, refresh, nil
}

// ReuseDetected 重放命中:吊销整个会话(安全处置,auth.md POST /refresh 表)。
func (s *Store) ReuseDetected(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if row, ok := s.sessions[sessionID]; ok && row.revokedAt == nil {
		t := s.now()
		row.revokedAt = &t
	}
}

// RevokeSession 吊销单个会话(logout);幂等。
func (s *Store) RevokeSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if row, ok := s.sessions[sessionID]; ok && row.revokedAt == nil {
		t := s.now()
		row.revokedAt = &t
	}
}

// SessionByAccessToken 实现 session.SessionStore(哈希查表)。
func (s *Store) SessionByAccessToken(_ context.Context, accessToken string) (session.SessionView, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.sessions[s.accessIndex[sha256Hex(accessToken)]]
	if !ok {
		return session.SessionView{}, false
	}
	return session.SessionView{
		ID:            row.id,
		AccountID:     row.accountID,
		AccountStatus: s.accounts[row.accountID].status,
		GameID:        row.gameID,
		Env:           row.env,
		DeviceID:      row.deviceID,
		AccessExpires: row.accessExpiresAt,
		Revoked:       row.revokedAt != nil,
	}, true
}

// --- 注销(v1.1,auth.md POST /account/delete) ---

// RequestDelete 注销受理:EMAIL + grace > 0 → 进入 PENDING_DELETE、吊销全部
// 会话、签发一次性撤销 token(重复受理重签,旧 token 立即失效);否则匿名化
// 立即删除(GUEST 恒立即,EMAIL 随管理面 grace=0 配置)。
// 返回:是否进入宽限期、明文 cancelToken(仅宽限期非空)、deletedAt(仅立即删)。
func (s *Store) RequestDelete(accountID string, grace time.Duration) (pending bool, cancelToken string, deletedAt *time.Time, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.accounts[accountID]
	if !ok {
		return false, "", nil, ErrNotFound
	}
	if acc.status != StatusActive && acc.status != StatusPendingDel {
		return false, "", nil, ErrDisabled
	}
	t := s.now()
	if acc.typ == AccountEmail && grace > 0 {
		acc.status = StatusPendingDel
		acc.deleteAfter = t.Add(grace)
		acc.updatedAt = t
		s.revokeAccountSessionsLocked(accountID, t)
		token, err := genToken()
		if err != nil {
			return false, "", nil, err
		}
		for h, row := range s.pendingDeletes { // 重签作废旧 token
			if row.accountID == accountID {
				delete(s.pendingDeletes, h)
			}
		}
		s.pendingDeletes[sha256Hex(token)] = &pendingDeleteRow{accountID: accountID, expiresAt: acc.deleteAfter}
		return true, token, nil, nil
	}
	if !s.anonymizeLocked(acc, t) {
		return false, "", nil, ErrNotFound
	}
	d := t
	return false, "", &d, nil
}

// revokeAccountSessionsLocked 吊销账号全部会话(受理即吊销,宽限期不例外);须持锁。
func (s *Store) revokeAccountSessionsLocked(accountID string, t time.Time) {
	for _, sess := range s.sessions {
		if sess.accountID == accountID && sess.revokedAt == nil {
			sess.revokedAt = &t
		}
	}
}

// ConsumeCancelToken 校验并消费撤销 token(一次性):匹配且未过期 → 返回
// accountID;不匹配/已用/已过期 → ErrDeleteTokenInvalid(auth.md v1.1)。
func (s *Store) ConsumeCancelToken(token string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.pendingDeletes[sha256Hex(token)]
	if !ok {
		return "", ErrDeleteTokenInvalid
	}
	delete(s.pendingDeletes, sha256Hex(token))
	if s.now().After(row.expiresAt) {
		return "", ErrDeleteTokenInvalid
	}
	return row.accountID, nil
}

// CancelDelete 撤销注销:账号回 ACTIVE;已吊销会话不恢复(需重登);
// 宽限期计时外状态不保留(再次申请注销重新走受理)。
func (s *Store) CancelDelete(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if acc, ok := s.accounts[accountID]; ok && acc.status == StatusPendingDel {
		acc.status = StatusActive
		acc.deleteAfter = time.Time{}
		acc.updatedAt = s.now()
	}
	for h, row := range s.pendingDeletes {
		if row.accountID == accountID {
			delete(s.pendingDeletes, h)
		}
	}
}

// anonymizeLocked 匿名化(auth.md v1.1 执行语义 = 匿名,非物理删行):清 PII
// (email、凭证、设备行与全部索引),id 保留供外键与审计,status=DELETED;
// 游客幂等索引一并清除——注销后同设备再进 = 新游客账号。须持锁。
func (s *Store) anonymizeLocked(acc *accountRow, t time.Time) bool {
	if acc.status == StatusDeleted {
		return false
	}
	if acc.email != "" {
		delete(s.emailIndex, acc.email)
	}
	acc.email = ""
	acc.status = StatusDeleted
	acc.deleteAfter = time.Time{}
	acc.updatedAt = t
	delete(s.credentials, acc.id)
	for h, row := range s.pendingDeletes { // 宽限随终态作废
		if row.accountID == acc.id {
			delete(s.pendingDeletes, h)
		}
	}
	for key, rowID := range s.deviceKeys {
		if row := s.devices[rowID]; row != nil && row.accountID == acc.id {
			delete(s.devices, rowID)
			delete(s.deviceKeys, key)
			if s.guestDevIndex[row.deviceID] == acc.id {
				delete(s.guestDevIndex, row.deviceID)
			}
		}
	}
	s.identities.RemoveAccount(acc.id) // v1.1 oauth:映射行随匿名化清理(oauth.md 扩展预留)
	return true
}

// FinalizeIfExpired 宽限期满终态化(PENDING_DELETE → 匿名化 DELETED);
// 触发时机为服务端实现细节(契约不约束)——此处惰性:login/bind/guest 前顺手。
// 返回是否执行了终态化。
func (s *Store) FinalizeIfExpired(accountID string) bool {
	s.mu.Lock()
	acc, ok := s.accounts[accountID]
	if !ok || acc.status != StatusPendingDel {
		s.mu.Unlock()
		return false
	}
	deadline := acc.deleteAfter
	s.mu.Unlock()
	if deadline.IsZero() || s.now().Before(deadline) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.accounts[accountID]
	if !ok || cur.status != StatusPendingDel || s.now().Before(cur.deleteAfter) {
		return false
	}
	return s.anonymizeLocked(cur, s.now())
}

// --- 第三方身份(v1.1,oauth.md;oauth.Store 适配,oauth.Handlers 共享端点处理) ---

func toOAuthAccount(a accountRow) oauth.Account {
	return oauth.Account{ID: a.id, Type: a.typ, Status: a.status, Email: a.email, CreatedAt: a.createdAt}
}

// FindOAuth 按 (provider, subject) 查已绑定账号;已匿名化账号的映射行随
// 匿名化清理,天然查不到(同 subject 再 exchange = 新 OAUTH 账号,同游客口径)。
func (s *Store) FindOAuth(provider, subject string) (oauth.Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.identities.Find(provider, subject)
	if !ok {
		return oauth.Account{}, false
	}
	acc, ok := s.accounts[row.AccountID]
	if !ok {
		return oauth.Account{}, false
	}
	return toOAuthAccount(*acc), true
}

// CreateOAuth 注册 type=OAUTH 账号并写入映射(契约 oauth.md:未命中即创建;
// 无凭证行,行为对齐 GUEST 最小凭证形态);可选绑定设备,超限整单回滚。
// 设备超限统一返回 oauth.ErrDeviceLimit(oauth 域哨兵,Handlers 统一映射)。
func (s *Store) CreateOAuth(provider, subject, deviceID, platform string) (oauth.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.now()
	id := s.newID("acc_", t)
	acc := &accountRow{id: id, typ: AccountOauth, status: StatusActive, createdAt: t, updatedAt: t}
	s.accounts[id] = acc
	if err := s.identities.Bind(id, provider, subject); err != nil {
		delete(s.accounts, id)
		return oauth.Account{}, err
	}
	if deviceID != "" {
		if _, err := s.bindDeviceLocked(acc, deviceID, platform, t); err != nil {
			s.identities.Unbind(id, provider)
			delete(s.accounts, id)
			if errors.Is(err, ErrDeviceLimit) {
				return oauth.Account{}, oauth.ErrDeviceLimit
			}
			return oauth.Account{}, err
		}
	}
	return toOAuthAccount(*acc), nil
}

// CreateOAuthSession oauth 域会话签发(复用 CreateSession,投影 oauth.Session)。
func (s *Store) CreateOAuthSession(accountID, gameID, env, deviceID string) (oauth.Session, error) {
	sess, access, refresh, err := s.CreateSession(accountID, gameID, env, deviceID)
	if err != nil {
		if errors.Is(err, ErrDisabled) {
			return oauth.Session{}, oauth.ErrDisabled
		}
		return oauth.Session{}, err
	}
	acc, ok := s.GetAccount(accountID)
	if !ok {
		return oauth.Session{}, ErrNotFound
	}
	return oauth.Session{
		Account:          toOAuthAccount(acc),
		AccessToken:      access,
		AccessExpiresAt:  sess.accessExpiresAt,
		RefreshToken:     refresh,
		RefreshExpiresAt: sess.refreshExpiresAt,
		DeviceID:         deviceID,
	}, nil
}

// BindOAuth 绑定第三方身份到账号(契约 oauth.md POST bind):同 provider 重复
// 绑定 → oauth.ErrProviderBound(COMMON_INVALID_ARGUMENT);subject 已属其他
// 账号 → oauth.ErrIdentityTaken(OAUTH_IDENTITY_TAKEN);宽限期账号拒绝(防御竞态,
// 受理即吊销会话,正常不可达)。
func (s *Store) BindOAuth(accountID, provider, subject string) (oauth.IdentityRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.accounts[accountID]
	if !ok {
		return oauth.IdentityRow{}, ErrNotFound
	}
	if acc.status == StatusPendingDel {
		return oauth.IdentityRow{}, oauth.ErrPendingDelete
	}
	if err := s.identities.Bind(accountID, provider, subject); err != nil {
		return oauth.IdentityRow{}, err
	}
	row, _ := s.identities.Find(provider, subject)
	return row, nil
}

// UnbindOAuth 解绑并删除映射行(幂等);解绑不影响账号本身(oauth.md)。
func (s *Store) UnbindOAuth(accountID, provider string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.identities.Unbind(accountID, provider)
}

// ListBindings 已绑定列表,createdAt 升序(不返回 subject)。
func (s *Store) ListBindings(accountID string) []oauth.IdentityRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.identities.ByAccount(accountID)
}
