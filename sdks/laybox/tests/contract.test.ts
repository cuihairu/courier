// 契约测试骨架:错误枚举/DTO 与 fixture 对齐断言(node:test,零依赖)。
// 运行:node --test tests/contract.test.ts(Node ≥ 23.6,原生 TS 直接跑)
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { ERROR_PREFIXES, ErrorSpecs, ErrorCode, errorCodeOf } from "../src/contract/errors.ts";
import type { ErrorEnvelope, Page, SuccessEnvelope } from "../src/contract/envelope.ts";

const fixtureUrl = new URL("../../../docs/contract/fixtures/", import.meta.url);
const errors = JSON.parse(readFileSync(new URL("errors.json", fixtureUrl), "utf8")) as {
  version: number;
  prefixes: string[];
  codes: { code: string; http: number; retryable: boolean }[];
};

test("错误码集合与契约 fixture 完全一致(双向)", () => {
  assert.equal(errors.version, 1);
  const enumValues = Object.values(ErrorCode);
  assert.equal(enumValues.length, errors.codes.length);
  assert.deepEqual([...enumValues].sort(), errors.codes.map((c) => c.code).sort());
});

test("冻结映射 wire/HTTP/retryable 与 fixture 一致", () => {
  for (const row of errors.codes) {
    const key = errorCodeOf(row.code);
    assert.ok(key, `缺码 ${row.code}`);
    const spec = ErrorSpecs[key];
    assert.equal(spec.wire, row.code, row.code);
    assert.equal(spec.http, row.http, row.code);
    assert.equal(spec.retryable, row.retryable, row.code);
  }
});

test("域前缀注册表一致", () => {
  assert.deepEqual([...ERROR_PREFIXES], errors.prefixes);
});

test("未知 wire code 容忍(落 undefined,兜底分支)", () => {
  assert.equal(errorCodeOf("NOT_A_CODE"), undefined);
});

test("失败信封 DTO wire key 对齐(error+traceId,互斥,无 data)", () => {
  const raw =
    '{"error":{"code":"AUTH_TOKEN_EXPIRED","message":"session expired","retryable":false},"traceId":"4bf92f3577b34da6a3ce929d0e0e4736"}';
  const env = JSON.parse(raw) as ErrorEnvelope;
  assert.deepEqual(Object.keys(env).sort(), ["error", "traceId"]);
  assert.deepEqual(Object.keys(env.error).sort(), ["code", "message", "retryable"]);
  assert.equal(env.error.retryable, false);
  assert.match(env.traceId, /^[0-9a-f]{32}$/);
  assert.ok(!("data" in env));
});

test("成功信封只有 data;分页 items/nextCursor(空串 = 末页)", () => {
  const env = JSON.parse('{"data":{"items":[1,2],"nextCursor":""}}') as SuccessEnvelope<Page<number>>;
  assert.deepEqual(Object.keys(env), ["data"]);
  assert.deepEqual(Object.keys(env.data).sort(), ["items", "nextCursor"]);
  assert.equal(env.data.nextCursor, "");
});
