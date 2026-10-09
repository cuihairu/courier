// store.go 扁平 map 存储(与自建的「分实体 map + 会话行表」刻意不同)。
//
// 会话事实(代际/吊销/refresh 过期)在单行 session + 签名令牌 claim;账号按 ID
// 单 map + email 索引 + 游客设备索引;设备按账号集合计数(上限语义与自建一致)。
// 读取一律返回值拷贝(锁外使用不竞态)。
package accountalt

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// 存储层错误(错误码由上层映射,契约 docs/contract/auth.md)。
var (
	ErrEmailTaken   = errors.New("accountalt: 邮箱已被注册")
	ErrDeviceLimit  = errors.New("accountalt: 设备数超限")
	ErrAlreadyBound = errors.New("accountalt: 账号已绑定邮箱")
	ErrDisabled     = errors.New("accountalt: 账号禁用")
	ErrNotFound     = errors.New("accountalt: 不存在")
)

// Options 与自建实现同旋钮(契约 auth.md:TTL/上限接入方可配,不冻结数值)。
type Options struct {
	AccessTTL     time.Duration // 默认 15m
	RefreshTTL    time.Duration // 默认 30d
	MaxDevices    int           // 默认 5
	Iterations    int           // PBKDF2 迭代,默认 210000(测试调低)
	RatePerMinute int           // 敏感端点每 IP 限流,默认 10
	RateBurst     int           // 默认 10
	Clock         func() time.Time
	SigningKey    SigningKey // 覆盖签名密钥(测试/多实例共享;非法即 panic 快速失败)
}

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
		o.Iterations = 210000
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

type altAccount struct {
	id        string
	typ       string // EMAIL | GUEST
	email     string // 绑定后非空
	status    string // ACTIVE | DISABLED
	passHash  []byte
	passSalt  []byte
	createdAt time.Time
}

type altSession struct {
	id             string
	accountID      string
	deviceID       string
	gameID         string
	env            string
	gen            int    // 当前 refresh 代际(首发=1,轮换 +1)
	accessNonce    string // 当前 access 令牌记号;轮换即换,旧令牌作废
	revoked        bool
	refreshExpires time.Time
	createdAt      time.Time
}

type altDevice struct {
	id        string
	deviceID  string // 客户端设备标识(行 id 与之不同,同自建语义)
	platform  string
	createdAt time.Time
}

type Store struct {
	mu       sync.RWMutex
	opts     *Options
	codec    *codec
	accounts map[string]*altAccount
	byEmail  map[string]string
	byGuest  map[string]string // deviceID → 游客账号 ID
	sessions map[string]*altSession
	devices  map[string]map[string]*altDevice // accountID → deviceID → 行
}

func NewStore(opts *Options) *Store {
	c, err := newCodec(opts.SigningKey)
	if err != nil {
		panic(fmt.Sprintf("accountalt: %v", err))
	}
	return &Store{
		opts:     opts,
		codec:    c,
		accounts: make(map[string]*altAccount),
		byEmail:  make(map[string]string),
		byGuest:  make(map[string]string),
		sessions: make(map[string]*altSession),
		devices:  make(map[string]map[string]*altDevice),
	}
}

func (s *Store) now() time.Time { return s.opts.Clock() }

func newID(prefix string, n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("accountalt: rand: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(b)
}

func (s *Store) CreateEmailAccount(email, password string) (altAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, dup := s.byEmail[email]; dup {
		return altAccount{}, ErrEmailTaken
	}
	hash, salt, err := hashPassword(password, s.opts.Iterations)
	if err != nil {
		return altAccount{}, err
	}
	acc := &altAccount{
		id: newID("acctalt", 12), typ: "EMAIL", email: email, status: "ACTIVE",
		passHash: hash, passSalt: salt, createdAt: s.now(),
	}
	s.accounts[acc.id] = acc
	s.byEmail[email] = acc.id
	return *acc, nil
}

func (s *Store) CreateGuestAccount(deviceID, platform string) altAccount {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byGuest[deviceID]; ok {
		return *s.accounts[id]
	}
	acc := &altAccount{
		id: newID("acctalt", 12), typ: "GUEST", status: "ACTIVE", createdAt: s.now(),
	}
	s.accounts[acc.id] = acc
	s.byGuest[deviceID] = acc.id
	return *acc
}

func (s *Store) FindGuestByDevice(deviceID string) (altAccount, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byGuest[deviceID]
	if !ok {
		return altAccount{}, false
	}
	return *s.accounts[id], true
}

func (s *Store) GetAccount(id string) (altAccount, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	acc, ok := s.accounts[id]
	if !ok {
		return altAccount{}, false
	}
	return *acc, true
}

func (s *Store) BindEmail(accountID, email, password string) (altAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.accounts[accountID]
	if !ok {
		return altAccount{}, ErrNotFound
	}
	if acc.typ != "GUEST" || acc.email != "" {
		return altAccount{}, ErrAlreadyBound
	}
	if _, dup := s.byEmail[email]; dup {
		return altAccount{}, ErrEmailTaken
	}
	hash, salt, err := hashPassword(password, s.opts.Iterations)
	if err != nil {
		return altAccount{}, err
	}
	acc.typ = "EMAIL"
	acc.email = email
	acc.passHash = hash
	acc.passSalt = salt
	s.byEmail[email] = acc.id
	return *acc, nil
}

func (s *Store) VerifyEmailPassword(email, password string) (altAccount, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byEmail[email]
	if !ok {
		return altAccount{}, false
	}
	acc := s.accounts[id]
	if !verifyPassword(password, acc.passSalt, acc.passHash, s.opts.Iterations) {
		return altAccount{}, false
	}
	return *acc, true
}

// EnsureBindDevice 幂等绑定;新设备超上限 → ErrDeviceLimit(语义同自建)。
func (s *Store) EnsureBindDevice(accountID, deviceID, platform string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	set, ok := s.devices[accountID]
	if !ok {
		set = make(map[string]*altDevice)
		s.devices[accountID] = set
	}
	if _, exists := set[deviceID]; exists {
		return nil
	}
	if len(set) >= s.opts.MaxDevices {
		return ErrDeviceLimit
	}
	set[deviceID] = &altDevice{
		id: newID("devalt", 8), deviceID: deviceID, platform: platform, createdAt: s.now(),
	}
	return nil
}

func (s *Store) ListDevices(accountID string) []altDevice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := s.devices[accountID]
	out := make([]altDevice, 0, len(set))
	for _, d := range set {
		out = append(out, *d)
	}
	return out
}

func (s *Store) UnbindDevice(accountID, deviceID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	set := s.devices[accountID]
	if _, ok := set[deviceID]; !ok {
		return false
	}
	delete(set, deviceID)
	return true
}

// RevokeDeviceSessions 吊销该账号该设备的全部会话(解绑语义:设备会话全死)。
func (s *Store) RevokeDeviceSessions(accountID, deviceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess.accountID == accountID && sess.deviceID == deviceID {
			sess.revoked = true
		}
	}
}

// CreateSession 首代会话(gen=1)并签发双令牌(会话事实在 claim,存储只留状态)。
func (s *Store) CreateSession(accountID, gameID, env, deviceID string) (sessID, access, refresh string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.accounts[accountID]; !ok {
		return "", "", "", ErrNotFound
	}
	now := s.now()
	sess := &altSession{
		id: newID("sessalt", 12), accountID: accountID, deviceID: deviceID,
		gameID: gameID, env: env, gen: 1, refreshExpires: now.Add(s.opts.RefreshTTL),
		createdAt: now,
	}
	s.sessions[sess.id] = sess
	n := nonce()
	sess.accessNonce = n
	return sess.id,
		s.codec.issueAccess(sess.id, accountID, deviceID, gameID, env, now.Add(s.opts.AccessTTL), n),
		s.codec.issueRefresh(sess.id, 1, sess.refreshExpires),
		nil
}

// Rotate 代际 +1 并签发新双令牌;调用方已核对代际与过期。
func (s *Store) Rotate(sessID string) (access, refresh string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessID]
	if !ok {
		return "", "", ErrNotFound
	}
	now := s.now()
	sess.gen++
	sess.refreshExpires = now.Add(s.opts.RefreshTTL)
	n := nonce()
	sess.accessNonce = n
	return s.codec.issueAccess(sess.id, sess.accountID, sess.deviceID, sess.gameID, sess.env, now.Add(s.opts.AccessTTL), n),
		s.codec.issueRefresh(sess.id, sess.gen, sess.refreshExpires),
		nil
}

func (s *Store) RevokeSession(sessID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessID]; ok {
		sess.revoked = true
	}
}

func (s *Store) Session(sessID string) (altSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[sessID]
	if !ok {
		return altSession{}, false
	}
	return *sess, true
}

func (s *Store) SetAccountStatus(accountID, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if acc, ok := s.accounts[accountID]; ok {
		acc.status = status
	}
}
