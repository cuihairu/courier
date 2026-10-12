// identities.go account_identities 映射表(契约 oauth.md「数据模型」)。
//
// 只存 provider + subject 映射,不存凭证原文、不向第三方回传 Courier 数据;
// (provider, subject) 全局唯一,(accountId, provider) 唯一。
package oauth

import (
	"errors"
	"sync"
	"time"
)

// 存储层错误(错误码由 Handlers 统一映射,契约 oauth.md「端点契约」)。
var (
	ErrIdentityTaken = errors.New("oauth: 该第三方身份已绑定其他账号")
	ErrProviderBound = errors.New("oauth: 账号已绑定该提供方")
	ErrPendingDelete = errors.New("oauth: 账号注销宽限期内")
	ErrDisabled      = errors.New("oauth: 账号禁用")
	ErrDeviceLimit   = errors.New("oauth: 设备数超限")
)

// IdentityRow 映射行:accountId → accounts.id;provider 为管理面注册集合的
// 开集字符串;subject 经第三方验证后取得(客户端不得自报)。
type IdentityRow struct {
	AccountID string
	Provider  string
	Subject   string
	CreatedAt time.Time
}

func identityKey(a, b string) string { return a + "\x00" + b }

// Identities account_identities 内存表(持久化随存储面工程替换,接口形状不变)。
// 独立锁域:宿主存储(account/accountalt)在自身锁内调用,表操作自旋加锁——
// 读写顺序由宿主编排(先账号后身份),跨表原子性由调用方保证。
type Identities struct {
	mu     sync.RWMutex
	byPair map[string]string            // provider\x00subject → accountID
	byAcct map[string]map[string]string // accountID → provider → subject
	rows   map[string]IdentityRow       // accountID\x00provider → 行
	now    func() time.Time
}

// NewIdentities 空表;now 供时间注入(测试时钟)。
func NewIdentities(now func() time.Time) *Identities {
	if now == nil {
		now = time.Now
	}
	return &Identities{
		byPair: make(map[string]string),
		byAcct: make(map[string]map[string]string),
		rows:   make(map[string]IdentityRow),
		now:    now,
	}
}

// Find 按 (provider, subject) 定位映射行。
func (t *Identities) Find(provider, subject string) (IdentityRow, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	id, ok := t.byPair[identityKey(provider, subject)]
	if !ok {
		return IdentityRow{}, false
	}
	return t.rows[identityKey(id, provider)], true
}

// Bind 建立映射:账号已绑该 provider → ErrProviderBound;
// (provider, subject) 已属其他账号 → ErrIdentityTaken。
func (t *Identities) Bind(accountID, provider, subject string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, dup := t.byAcct[accountID][provider]; dup {
		return ErrProviderBound
	}
	if other, taken := t.byPair[identityKey(provider, subject)]; taken && other != accountID {
		return ErrIdentityTaken
	}
	row := IdentityRow{AccountID: accountID, Provider: provider, Subject: subject, CreatedAt: t.now()}
	t.byPair[identityKey(provider, subject)] = accountID
	if t.byAcct[accountID] == nil {
		t.byAcct[accountID] = make(map[string]string)
	}
	t.byAcct[accountID][provider] = subject
	t.rows[identityKey(accountID, provider)] = row
	return nil
}

// ByAccount 账号的全部映射行,按 createdAt 升序(契约 GET bindings)。
func (t *Identities) ByAccount(accountID string) []IdentityRow {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]IdentityRow, 0, len(t.byAcct[accountID]))
	for _, row := range t.rows {
		if row.AccountID == accountID {
			out = append(out, row)
		}
	}
	for i := 1; i < len(out); i++ { // 插入排序:行数小(每账号绑定数有限)
		for j := i; j > 0 && out[j].CreatedAt.Before(out[j-1].CreatedAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Unbind 删除 (accountID, provider) 映射;返回是否删除了行(幂等)。
func (t *Identities) Unbind(accountID, provider string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := identityKey(accountID, provider)
	row, ok := t.rows[key]
	if !ok {
		return false
	}
	delete(t.rows, key)
	delete(t.byAcct[accountID], provider)
	if len(t.byAcct[accountID]) == 0 {
		delete(t.byAcct, accountID)
	}
	delete(t.byPair, identityKey(row.Provider, row.Subject))
	return true
}

// RemoveAccount 账号匿名化级联:清该账号全部映射行(契约 oauth.md「扩展与演进」:
// account_identities 行随账号匿名化清理,accounts.id 保留)。
func (t *Identities) RemoveAccount(accountID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, row := range t.rows {
		if row.AccountID == accountID {
			delete(t.rows, key)
			delete(t.byPair, identityKey(row.Provider, row.Subject))
		}
	}
	delete(t.byAcct, accountID)
}
