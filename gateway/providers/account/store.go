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
	"github.com/cuihairu/courier/gateway/session"
)

// 存储层错误(错误码由上层映射,见 docs/contract/auth.md)。
var (
	ErrEmailTaken   = errors.New("account: 邮箱已被注册")
	ErrDeviceLimit  = errors.New("account: 设备数超限")
	ErrNotFound     = errors.New("account: 不存在")
	ErrAlreadyBound = errors.New("account: 账号已绑定邮箱")
	ErrDisabled     = errors.New("account: 账号禁用")
)

// Options 存储与会话参数(契约 auth.md:TTL/上限接入方可配,契约不冻结数值)。
type Options struct {
	AccessTTL     time.Duration // 默认 15m
	RefreshTTL    time.Duration // 默认 30d
	MaxDevices    int           // 默认 5
	Iterations    int           // PBKDF2 迭代,默认 210000(测试调低)
	RatePerMinute int           // 敏感端点每 IP 限流,默认 10
	RateBurst     int           // 默认 10
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
	if o.Clock == nil {
		o.Clock = time.Now
	}
}

// 账号/会话状态枚举(契约 auth.md「数据模型」)。
const (
	AccountEmail   = "EMAIL"
	AccountGuest   = "GUEST"
	StatusActive   = "ACTIVE"
	StatusDisabled = "DISABLED"
)

// 四表行结构,字段口径与 docs/contract/auth.md「数据模型」一致。
type accountRow struct {
	id        string // acc_<uuidv7>
	typ       string // EMAIL | GUEST
	email     string // GUEST 为空
	status    string // ACTIVE | DISABLED
	createdAt time.Time
	updatedAt time.Time
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

// Store 内存存储,四表结构与 auth.md 附录 DDL 同构;粗粒度互斥锁。
// 持久化(SQLite/Postgres)随 M1 后续工程替换,接口形状不变。
type Store struct {
	mu   sync.Mutex
	opts Options

	accounts    map[string]*accountRow
	credentials map[string]*credentialRow
	sessions    map[string]*sessionRow
	devices     map[string]*deviceRow

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
	return &Store{
		opts:           opts,
		accounts:       make(map[string]*accountRow),
		credentials:    make(map[string]*credentialRow),
		sessions:       make(map[string]*sessionRow),
		devices:        make(map[string]*deviceRow),
		emailIndex:     make(map[string]string),
		guestDevIndex:  make(map[string]string),
		deviceKeys:     make(map[string]string),
		accessIndex:    make(map[string]string),
		refreshIndex:   make(map[string]string),
		prevRefreshIdx: make(map[string]string),
	}
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
