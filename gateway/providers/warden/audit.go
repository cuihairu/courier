// 审计留痕半边(契约 F33「审计与合规留痕」的进程内接缝):实名域关键动作产出
// 结构化事件,经 AuditSink 交给接入方审计存储。留痕面只是接缝——换 sink =
// 换配置,网关不含存储;落库(保留策略 / 查询 / 合规导出)由实现方负责。
//
// 红线(realname.md「字段脱敏与数据红线」):AuditEvent 只含账号维度元数据,
// 严禁携带姓名 / 证件号明文或指纹。留什么、存哪、保多久由实现方决定;本包只
// 负责「在动作发生时通知」。纯读(status)不留痕——它不产生合规决策,也不改状态。
package warden

import (
	"encoding/json"
	"log"
	"time"
)

// 留痕动作(账号维度;与端点一一对应,status 除外)。
const (
	AuditActionVerify         = "verify"
	AuditActionCurfew         = "curfew"
	AuditActionChargeCheck    = "charge_check"
	AuditActionPlaytimeReport = "playtime_report"
)

// 留痕结果(verify 直接回传实名状态,其余按动作域取值)。
const (
	AuditOutcomePlayable = "playable"
	AuditOutcomeDenied   = "denied"
	AuditOutcomeAllowed  = "allowed"
	AuditOutcomeOK       = "ok"
)

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
