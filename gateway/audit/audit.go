// 审计留痕接缝(契约 F33「审计与合规留痕」的进程内共享面):实名域关键动作产出
// 结构化事件,经 AuditSink 交给接入方审计存储。留痕面只是接缝——换 sink =
// 换配置,网关不含存储;落库(保留策略 / 查询 / 合规导出)由实现方负责。
//
// 本包是共享接缝而非某个 Provider 的私有件:实名域两个实现(warden 自建核验 /
// wardenalt 托管核验)都经本包留痕——「同契约可换」要求可换实现的行为面一致,
// 审计事件面是其中之一(切换 Provider 不得静默丢合规留痕)。Provider 互不
// import(layers.md L4),故接缝必须独立成包,不能挂在任一 Provider 下。
//
// 红线(realname.md「字段脱敏与数据红线」):AuditEvent 只含账号维度元数据,
// 严禁携带姓名 / 证件号明文或指纹。留什么、存哪、保多久由实现方决定;本包只
// 负责「在动作发生时通知」。纯读(status)不留痕——它不产生合规决策,也不改状态。
package audit

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// 留痕动作(账号维度;与端点一一对应,status 除外)。
const (
	AuditActionVerify         = "verify"
	AuditActionCurfew         = "curfew"
	AuditActionChargeCheck    = "charge_check"
	AuditActionPlaytimeReport = "playtime_report"
)

// consent 组留痕动作(realname.md v1.1,契约逐字命名——与 v1 短名风格不同,
// v1.1 注册名即带 realname.consent 前缀)。账号维度;出生日期与家长身份输入
// 永不进事件(同 v1 红线)。
const (
	AuditActionConsentDeclared        = "realname.consent.declared"
	AuditActionConsentParentRequested = "realname.consent.parent_requested"
	AuditActionConsentGranted         = "realname.consent.granted"
	AuditActionConsentDenied          = "realname.consent.denied"
	AuditActionConsentRevoked         = "realname.consent.revoked"
)

// 留痕结果(verify 直接回传实名状态,其余按动作域取值)。
const (
	AuditOutcomePlayable = "playable"
	AuditOutcomeDenied   = "denied"
	AuditOutcomeAllowed  = "allowed"
	AuditOutcomeOK       = "ok"
)

// defaultHTTPTimeout HTTPSink 缺省超时:审计在请求路径上必须有界,不拖垮
// 实名动作本身(与 wardenalt.WebhookVerifier 的 5s 核验超时同思路,更短)。
const defaultHTTPTimeout = 1 * time.Second

// AuditSink 审计留痕接收面。实现方接接入方审计存储(日志采集 / DB / SIEM)。
// Record 在请求路径上同步调用:实现方必须自行保证不阻塞(缓冲或异步落库)。
type AuditSink interface {
	Record(event AuditEvent)
}

// AuditEvent 审计事件(账号维度元数据,无身份输入——红线)。
type AuditEvent struct {
	TraceID   string    `json:"traceId"`   // 网关 trace,与请求/错误信封关联
	AccountID string    `json:"accountId"` // 账号
	Action    string    `json:"action"`    // AuditAction*
	Outcome   string    `json:"outcome"`   // AuditOutcome* 或实名状态
	At        time.Time `json:"at"`        // 事件时刻(Clock 口径)
}

// LogSink 日志半边:每事件一行结构化 JSON 经标准库日志输出,接入方按部署面
// 采集(容器 stdout → 日志系统)。不配置 AuditSink = 不产出任何审计事件。
type LogSink struct {
	// Logger 输出目标(nil = 标准日志)。
	Logger *log.Logger
}

func (s LogSink) Record(e AuditEvent) {
	l := s.Logger
	if l == nil {
		l = log.Default()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.Printf("courier realname audit %s", b)
}

// HTTPSink 直写半边:每事件一条 POST 发往接入方端点(SIEM 摄取端 / 自建落库
// 代理 / 日志接收端),满足 F33「可追溯日志」的举证面——网关仍不持有存储。
// 端点与 Bearer 凭证均为部署面注入,形状与 wardenalt.WebhookVerifier 同构。
//
// 尽力交付(best-effort):超时、网络失败与非 2xx 只记日志,不重试、不阻断、
// 不影响实名请求结果——审计是旁路留痕,不能反过来打断合规动作本身;失败可见性
// 由 Logger 承担(接入方监控采集该日志)。同步但有界:请求路径上的耗时上限 =
// Client 超时,缺省 1s。
type HTTPSink struct {
	// Endpoint 接入方端点(空 = 不产出,等价未配置 sink)。
	Endpoint string
	// Token 可选 Bearer 凭证。
	Token string
	// Client HTTP 客户端;nil = 缺省 1s 超时。
	Client *http.Client
	// Logger 失败日志输出目标(nil = 标准日志)。
	Logger *log.Logger
}

func (s HTTPSink) Record(e AuditEvent) {
	l := s.Logger
	if l == nil {
		l = log.Default()
	}
	if s.Endpoint == "" {
		return
	}
	body, err := json.Marshal(e)
	if err != nil {
		l.Printf("courier realname audit sink: marshal %v", err)
		return
	}
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	req, err := http.NewRequest(http.MethodPost, s.Endpoint, bytes.NewReader(body))
	if err != nil {
		l.Printf("courier realname audit sink: endpoint %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if s.Token != "" {
		req.Header.Set("Authorization", "Bearer "+s.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		l.Printf("courier realname audit sink: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode > 299 {
		l.Printf("courier realname audit sink: status %d", resp.StatusCode)
	}
}
