// tokens.go 签名令牌编解码:HMAC-SHA256 自验证令牌(与自建的 64-hex 不透明
// 随机令牌刻意不同——契约不冻结令牌格式,只冻结行为)。
//
// 形状:at1.<b64url(payload)>.<hex hmac> / rt1.<b64url(payload)>.<hex hmac>
// access payload: v1|sessionID|accountID|deviceID|gameID|env|expUnix|nonce
// refresh payload: v1|sessionID|gen|expUnix|nonce
// nonce 8B 随机:同秒轮换/同 claim 重签也产出不同令牌(轮换=新令牌是行为契约)。
package accountalt

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	accessPrefix  = "at1"
	refreshPrefix = "rt1"
	secretLen     = 32
)

// ErrTokenMalformed 令牌形状/签名不符(验证方按「查无」处理)。
var ErrTokenMalformed = errors.New("accountalt: 令牌形状或签名不符")

// SigningKey 覆盖签名密钥(测试/多实例共享密钥用;缺省随机 32B)。
type SigningKey []byte

type codec struct {
	secret []byte
}

func newCodec(key SigningKey) (*codec, error) {
	if len(key) == 0 {
		secret := make([]byte, secretLen)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("accountalt: 生成签名密钥: %w", err)
		}
		return &codec{secret: secret}, nil
	}
	if len(key) < 16 {
		return nil, errors.New("accountalt: 签名密钥至少 16B")
	}
	return &codec{secret: append([]byte(nil), key...)}, nil
}

func (c *codec) sign(payload string) string {
	mac := hmac.New(sha256.New, c.secret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// nonce 8B 随机 hex(签发即不同,保证轮换产出新令牌)。
func nonce() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// rand 失败即签发失败,panic 比静默复用令牌好
		panic(fmt.Sprintf("accountalt: rand: %v", err))
	}
	return hex.EncodeToString(b)
}

// issueAccess 签发 access 令牌(claim 内嵌会话事实,验证无需查表;
// n 为当前 access 记号,由存储侧持有——轮换即换,旧令牌随之作废)。
func (c *codec) issueAccess(sessID, accountID, deviceID, gameID, env string, exp time.Time, n string) string {
	payload := strings.Join([]string{
		"v1", sessID, accountID, deviceID, gameID, env,
		strconv.FormatInt(exp.Unix(), 10), n,
	}, "|")
	enc := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return accessPrefix + "." + enc + "." + c.sign(enc)
}

// issueRefresh 签发 refresh 令牌(代际计数内嵌,重放检测无需查历史行)。
func (c *codec) issueRefresh(sessID string, gen int, exp time.Time) string {
	payload := strings.Join([]string{
		"v1", sessID, strconv.Itoa(gen), strconv.FormatInt(exp.Unix(), 10), nonce(),
	}, "|")
	enc := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return refreshPrefix + "." + enc + "." + c.sign(enc)
}

type accessClaims struct {
	sessionID  string
	accountID  string
	deviceID   string
	gameID     string
	env        string
	expiresAt  time.Time
	nonce      string // 当前 access 唯一记号;轮换后旧令牌作废(nonce 失配即失效)
}

type refreshClaims struct {
	sessionID string
	gen       int
	expiresAt time.Time
}

// parseAccess 验签 + 解 claim;签名不符/形状错一律 ErrTokenMalformed。
func (c *codec) parseAccess(token string) (accessClaims, error) {
	enc, err := c.parse(accessPrefix, token)
	if err != nil {
		return accessClaims{}, err
	}
	f := strings.Split(enc, "|")
	if len(f) != 8 || f[0] != "v1" {
		return accessClaims{}, ErrTokenMalformed
	}
	exp, err := strconv.ParseInt(f[6], 10, 64)
	if err != nil {
		return accessClaims{}, ErrTokenMalformed
	}
	return accessClaims{
		sessionID: f[1], accountID: f[2], deviceID: f[3],
		gameID: f[4], env: f[5], expiresAt: time.Unix(exp, 0), nonce: f[7],
	}, nil
}

// parseRefresh 验签 + 解 claim。
func (c *codec) parseRefresh(token string) (refreshClaims, error) {
	enc, err := c.parse(refreshPrefix, token)
	if err != nil {
		return refreshClaims{}, err
	}
	f := strings.Split(enc, "|")
	if len(f) != 5 || f[0] != "v1" {
		return refreshClaims{}, ErrTokenMalformed
	}
	gen, err := strconv.Atoi(f[2])
	if err != nil {
		return refreshClaims{}, ErrTokenMalformed
	}
	exp, err := strconv.ParseInt(f[3], 10, 64)
	if err != nil {
		return refreshClaims{}, ErrTokenMalformed
	}
	return refreshClaims{sessionID: f[1], gen: gen, expiresAt: time.Unix(exp, 0)}, nil
}

// parse 拆三段 + 常数时间验签。
func (c *codec) parse(prefix, token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != prefix {
		return "", ErrTokenMalformed
	}
	want := c.sign(parts[1])
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return "", ErrTokenMalformed
	}
	enc, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrTokenMalformed
	}
	return string(enc), nil
}
