# 契约 Fixtures(机器可读快照)

> 本目录是契约文档的**机器可读快照**,供 [tools/contractgen](https://github.com/cuihairu/courier/tree/main/tools/contractgen) 生成六端契约源(C# / TS / C++ / GDScript),漂移即测试红。约定:**文档先行,fixture 跟随,生成物不手改**。

| 文件 | 内容 | 消费方 |
| --- | --- | --- |
| `errors.json` | 错误码表(version/http/retryable + 域前缀) | contractgen 生成各端 `ErrorCode`/`ErrorSpecs`;gateway `codeTable` 同步测试 |
| `scope.json` | scope header 常量与环境枚举 | 各端 scope 常量生成 |
| `dto.json` | 信封与分页 wire key | 各端 `Envelope`/`Page` 类型生成 |

变更流程:改 [errors.md](../errors.md) 等文档 → 同步改 fixture(版本只加)→ `go run ./tools/contractgen -write` 重生成 → 全端契约测试绿才算同步完成(见[总纲 · 变更流程](../index.md))。
