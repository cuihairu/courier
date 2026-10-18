package middleware

import "net/http"

// MaxBodyBytes 请求体统一上限(1MiB;审核 2026-10-18 表一#2:公开 JSON 端点
// 无大小上限 = 任意大 body 打满内存)。契约内最大合法载荷是支持工单 context
// 开集,1MiB 足够并封顶单请求内存;超限在读取时报 *http.MaxBytesError,
// 由各 decode 处转 400,连接随之断开。
const MaxBodyBytes int64 = 1 << 20

// MaxBody 请求体大小上限:包一层 http.MaxBytesReader,挂在链最外层
// (先于业务 decode 生效,GET 等无 body 请求零开销)。
func MaxBody(limit int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limit > 0 && r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}
