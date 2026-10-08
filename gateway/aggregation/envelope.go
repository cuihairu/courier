// Package aggregation 按契约输出响应信封(源:docs/contract/primitives.md、errors.md)。
//
// 成功 {"data":...};失败 {"error":{code,message,retryable},"traceId":...};二者互斥。
// 错误码 → HTTP/retryable 映射转录自 docs/contract/errors.md v1 冻结表,
// 域业务码随该域契约冻结时在 codeTable 登记。
package aggregation

import (
	"encoding/json"
	"net/http"
)

// 契约冻结错误码(源:docs/contract/errors.md 错误码表 v1)。
const (
	CodeInternal           = "COMMON_INTERNAL"
	CodeInvalidArgument    = "COMMON_INVALID_ARGUMENT"
	CodeUnauthenticated    = "COMMON_UNAUTHENTICATED"
	CodePermissionDenied   = "COMMON_PERMISSION_DENIED"
	CodeNotFound           = "COMMON_NOT_FOUND"
	CodeCapabilityDisabled = "COMMON_CAPABILITY_DISABLED"
	CodeUnavailable        = "COMMON_UNAVAILABLE"
	CodeRateLimited        = "RATE_LIMITED"
	CodeScopeMismatch      = "SCOPE_MISMATCH"

	CodeInvalidCredentials = "AUTH_INVALID_CREDENTIALS"
	CodeTokenExpired       = "AUTH_TOKEN_EXPIRED"
	CodeTokenRevoked       = "AUTH_TOKEN_REVOKED"
	CodeRefreshReused      = "AUTH_REFRESH_REUSED"
	CodeAccountDisabled    = "AUTH_ACCOUNT_DISABLED"
	CodeDeviceLimit        = "AUTH_DEVICE_LIMIT"
	CodeEmailTaken         = "AUTH_EMAIL_TAKEN"
	CodeSessionConflict    = "SESSION_CONFLICT"

	CodeRealNameRequired = "REALNAME_REQUIRED"
	CodeRealNameRejected = "REALNAME_REJECTED"
	CodeRealNamePending  = "REALNAME_PENDING_REVIEW"

	CodeConfigNotFound     = "CONFIG_NOT_FOUND"
	CodeMaintenance        = "APP_MAINTENANCE"
	CodeVersionUnsupported = "APP_VERSION_UNSUPPORTED"
)

// codeSpec 冻结映射:code → HTTP 状态 + retryable。
type codeSpec struct {
	status    int
	retryable bool
}

// codeTable 转录自 docs/contract/errors.md 错误码表 v1;表内 code 语义冻结。
var codeTable = map[string]codeSpec{
	CodeInternal:           {http.StatusInternalServerError, true},
	CodeInvalidArgument:    {http.StatusBadRequest, false},
	CodeUnauthenticated:    {http.StatusUnauthorized, false},
	CodePermissionDenied:   {http.StatusForbidden, false},
	CodeNotFound:           {http.StatusNotFound, false},
	CodeCapabilityDisabled: {http.StatusNotImplemented, false},
	CodeUnavailable:        {http.StatusServiceUnavailable, true},
	CodeRateLimited:        {http.StatusTooManyRequests, true},
	CodeScopeMismatch:      {http.StatusBadRequest, false},

	CodeInvalidCredentials: {http.StatusUnauthorized, false},
	CodeTokenExpired:       {http.StatusUnauthorized, false},
	CodeTokenRevoked:       {http.StatusUnauthorized, false},
	CodeRefreshReused:      {http.StatusUnauthorized, false},
	CodeAccountDisabled:    {http.StatusForbidden, false},
	CodeDeviceLimit:        {http.StatusForbidden, false},
	CodeEmailTaken:         {http.StatusConflict, false},
	CodeSessionConflict:    {http.StatusConflict, false},

	CodeRealNameRequired: {http.StatusForbidden, false},
	CodeRealNameRejected: {http.StatusForbidden, false},
	CodeRealNamePending:  {http.StatusForbidden, false},

	CodeConfigNotFound:     {http.StatusNotFound, false},
	CodeMaintenance:        {http.StatusServiceUnavailable, false},
	CodeVersionUnsupported: {http.StatusUpgradeRequired, false},
}

// Error 契约错误体(errors.md「错误体」)。
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// failureEnvelope 失败信封:仅 error + traceId,与 data 互斥。
type failureEnvelope struct {
	Error   Error  `json:"error"`
	TraceID string `json:"traceId"`
}

// WriteData 写成功信封:{"data":...}(primitives.md「成功响应只有 data」)。
// data 为 nil 时输出 {"data":null} 以外的空对象语义由调用方负责(传 struct{}{} 即 {})。
func WriteData(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// WriteError 写失败信封:{"error":{code,message,retryable},"traceId":...},
// HTTP 状态与 retryable 由 codeTable 冻结映射决定;未登记的 code 保守按 500 处理。
// traceId 必填:契约要求失败响应 body 必带 32-hex traceId。
func WriteError(w http.ResponseWriter, traceID, code, message string) {
	spec, ok := codeTable[code]
	if !ok {
		spec = codeSpec{http.StatusInternalServerError, true}
	}
	writeJSON(w, spec.status, failureEnvelope{
		Error:   Error{Code: code, Message: message, Retryable: spec.retryable},
		TraceID: traceID,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		// 信封只含字符串/布尔,marshal 不应失败;兜底写裸 500。
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"COMMON_INTERNAL","message":"marshal failed","retryable":true},"traceId":""}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
