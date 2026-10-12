// 工单附件(support.md v1.1「工单附件」节,F44):单端点申请直传,网关不落盘二进制。
// 客户端拿 uploadUrl 直传存储(HTTP PUT,不经网关);存储本体挡在 Signer 后
// (换 S3 / OSS / 本地静态服务器 = 换配置,契约不动)。默认未配置 = fail-closed 501
// COMMON_CAPABILITY_DISABLED(同 oauth 校验器注册表口径,不内置假存储到生产 main.go)。
package croupier

import (
	"net/http"
	"time"

	"github.com/cuihairu/courier/gateway/aggregation"
	"github.com/cuihairu/courier/gateway/auth"
	"github.com/cuihairu/courier/gateway/middleware"
)

const (
	// 附件限额缺省(契约 support.md v1.1,管理面可配)。
	defaultMaxAttachmentsPerTicket = 3
	defaultMaxAttachmentBytes      = 10 * 1024 * 1024 // 10 MiB
	defaultAttachmentTTL           = 10 * time.Minute
)

// 附件 contentType allowlist 缺省(管理面可换;不在表内 → COMMON_INVALID_ARGUMENT)。
var defaultAttachmentTypes = map[string]bool{
	"image/png":       true,
	"image/jpeg":      true,
	"text/plain":      true,
	"application/zip": true,
	"application/pdf": true,
}

// Signer 存储面抽象:生成短时效预签名 URL。实现方接 S3 / OSS / 本地静态服务器。
type Signer interface {
	// SignPresign 为 PUT 直传与 GET 下载签发同 key 的短时效 URL 与必带 headers。
	// base 由 Provider 生成(不暴露对象存储布局);ttl 按配置给出。
	SignPresign(base string, ttl time.Duration) (url string, headers map[string]string)
}

// attachmentRequest 申请请求体(契约 support.md v1.1)。
type attachmentRequest struct {
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	Kind        string `json:"kind"`
}

// attachmentUploadDTO 申请响应 data(契约 support.md v1.1)。
type attachmentUploadDTO struct {
	AttachmentID string            `json:"attachmentId"`
	UploadURL    string            `json:"uploadUrl"`
	Method       string            `json:"method"`
	Headers      map[string]string `json:"headers"`
	ExpiresAt    string            `json:"expiresAt"`
}

// attachmentDTO 工单详情 attachments[] 项(不返回服务端存储路径)。
type attachmentDTO struct {
	ID          string `json:"id"`
	FileName    string `json:"fileName"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
	Kind        string `json:"kind,omitempty"`
	CreatedAt   string `json:"createdAt"`
	DownloadURL string `json:"downloadUrl,omitempty"`
}

// attachmentRecord 附件登记行(createdAt 追加序;downloadURL 与登记同批签发)。
type attachmentRecord struct {
	id          string
	fileName    string
	contentType string
	sizeBytes   int64
	kind        string
	downloadURL string
	createdAt   time.Time
}

// handleRequestUpload POST /v1/support/tickets/{ticketId}/attachments(Bearer):
// 登记附件并返回短时效预签名直传 URL。校验顺序 = 参数 → 体积上限 → 归属/状态 →
// 计数上限 → 存储可用性,保证错误码稳定可断言。
func (p *SupportProvider) handleRequestUpload(w http.ResponseWriter, r *http.Request, ticketID string) {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		p.writeUnauthenticated(w, r)
		return
	}
	var req attachmentRequest
	if !p.decode(w, r, &req) {
		return
	}
	if err := validAttachmentRequest(req, p.allowedAttachmentTypes()); err != nil {
		p.writeInvalid(w, r, err.Error())
		return
	}
	if req.SizeBytes > int64(p.maxAttachmentBytes()) {
		p.writeError(w, middleware.TraceID(r.Context()),
			aggregation.CodeSupportAttachmentTooLarge, "attachment too large")
		return
	}

	p.mu.Lock()
	rec, owned := p.tickets[ticketID]
	if !owned || rec.owner != id.AccountID {
		p.mu.Unlock()
		p.writeNotFound(w, r)
		return
	}
	if rec.status == "CLOSED" {
		p.mu.Unlock()
		p.writeError(w, middleware.TraceID(r.Context()),
			aggregation.CodeSupportTicketClosed, "ticket closed")
		return
	}
	if len(rec.attachments) >= p.maxAttachmentLimit() {
		p.mu.Unlock()
		p.writeError(w, middleware.TraceID(r.Context()),
			aggregation.CodeSupportAttachmentLimit, "attachment limit reached")
		return
	}
	if p.signer == nil { // 未配置存储:登记不落地,fail-closed
		p.mu.Unlock()
		p.writeError(w, middleware.TraceID(r.Context()),
			aggregation.CodeCapabilityDisabled, "attachment storage not configured")
		return
	}

	ttl := p.attachmentTTL()
	now := p.clock()
	recID := "att_" + newUUIDv7(now)
	// 存储 key 含账号隔离段:玩家只见自己的工单附件,坐席侧走管理面。
	base := "support/uploads/" + rec.owner + "/" + recID
	upURL, upHeaders := p.signer.SignPresign(base, ttl)
	downURL, _ := p.signer.SignPresign(base, ttl)
	rec.attachments = append(rec.attachments, attachmentRecord{
		id:          recID,
		fileName:    req.FileName,
		contentType: req.ContentType,
		sizeBytes:   req.SizeBytes,
		kind:        req.Kind,
		downloadURL: downURL,
		createdAt:   now,
	})
	rec.updatedAt = now
	p.mu.Unlock()

	aggregation.WriteData(w, attachmentUploadDTO{
		AttachmentID: recID,
		UploadURL:    upURL,
		Method:       "PUT",
		Headers:      upHeaders,
		ExpiresAt:    formatMilli(now.Add(ttl)),
	})
}

// attachmentsOf 工单详情 attachments[](追加序 = createdAt 升序;对象未直传完成前
// 由存储返回 404,网关不参与)。
func (p *SupportProvider) attachmentsOf(rec *ticketRecord) []attachmentDTO {
	out := make([]attachmentDTO, 0, len(rec.attachments))
	for _, a := range rec.attachments {
		out = append(out, attachmentDTO{
			ID:          a.id,
			FileName:    a.fileName,
			ContentType: a.contentType,
			SizeBytes:   a.sizeBytes,
			Kind:        a.kind,
			CreatedAt:   formatMilli(a.createdAt),
			DownloadURL: a.downloadURL,
		})
	}
	return out
}

// validAttachmentRequest 参数校验(契约:fileName 1–255 字符;contentType 须在
// allowlist 内;sizeBytes 为正;kind 可选 IMAGE/LOG/OTHER)。
func validAttachmentRequest(req attachmentRequest, allowed map[string]bool) error {
	if n := len(req.FileName); n < 1 || n > 255 {
		return errInvalid("fileName 1-255 字符")
	}
	if req.ContentType == "" || !attachmentTypeAllowed(req.ContentType, allowed) {
		return errInvalid("contentType 不在允许列表内")
	}
	if req.SizeBytes <= 0 {
		return errInvalid("sizeBytes 须为正整数")
	}
	switch req.Kind {
	case "", "IMAGE", "LOG", "OTHER":
	default:
		return errInvalid("kind 须为 IMAGE/LOG/OTHER")
	}
	return nil
}

func attachmentTypeAllowed(ct string, allowed map[string]bool) bool {
	if allowed != nil {
		if _, ok := allowed[ct]; ok {
			return true
		}
	}
	return defaultAttachmentTypes[ct]
}

func (p *SupportProvider) writeError(w http.ResponseWriter, traceID, code, msg string) {
	aggregation.WriteError(w, traceID, code, msg)
}
