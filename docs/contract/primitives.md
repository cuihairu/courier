# 基元契约:Request / Response / Pagination / Timestamp / ID / TraceID

> 状态:Draft v0。本文档定义所有域契约共用的线格式基元;域契约不得另造。

## Response 信封

成功:

```json
{ "data": { } }
```

失败(结构见 [errors.md](./errors.md)):

```json
{
  "error": { "code": "AUTH_TOKEN_EXPIRED", "message": "session expired", "retryable": false },
  "traceId": "4bf92f3577b34da6a3ce929d0e0e4736"
}
```

- 成功响应只有 `data`;失败响应只有 `error` + `traceId`,二者互斥。
- 列表响应是 `data` 内含分页对象(见下)。

## ID

- 一律字符串,服务端生成,UUIDv7(时间有序,利于索引)。
- 对外不暴露自增 ID。
- 域前缀可选(如 `acc_`、`ses_`、`ply_`、`tkt_`、`ord_`),由域契约声明;无前缀也算合法 ID。

## Timestamp

- RFC 3339 UTC,毫秒精度:`2026-10-08T03:20:00.123Z`。
- 字段名以 `_at` 结尾(`created_at`、`expires_at`)。
- 时长/间隔用毫秒整数,字段名以 `_ms` 结尾(`timeout_ms`)。

## TraceID

- 32 位 hex,兼容 W3C Trace Context(`traceparent` header 可选支持)。
- 每个响应 body 必带 `traceId`;客户端上报问题时应附带它。
- 请求可携带 `X-Request-Id`(客户端生成,幂等去重可用),网关原样回传。

## Pagination

Cursor 基,不用页码:

- 请求:`?limit=20&cursor=<opaque>`;`limit` 默认 20,最大 100。
- 响应:

```json
{ "data": { "items": [ ], "nextCursor": "<opaque>" } }
```

- `nextCursor` 为空字符串或缺失 = 末页。cursor 是不透明字符串,客户端不得解析。

## 空值与枚举

- 可选字段缺省即未设置;不区分「null」与「缺失」,客户端按缺失处理。
- 布尔字段两态,不用三态。
- 枚举一律大写下划线字符串(`"PENDING_REVIEW"`),各端映射为本端枚举;禁止魔术数字上连线。
