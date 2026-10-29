- [x] 审核整改(2026-10-18 独立审核,见 docs/review/luna-review-2026-10-18.md):
      表一缺陷 6 项全修,表二不一致 5 项全消,表三能力 6 项全部登记评估不实现:
      1) 表一:#1 webhook 强制 HTTPS+重定向 fail-closed(wardenalt.go,含测试);
         #2 请求体 1MiB 上限(middleware/bodylimit.go + main.go 最外层 + account decode
         MaxBytesError 分流,含端到端测试);#3 http.Server 显式超时(main.go newServer:
         读 10s/60s/空闲 120s,写 0 保 SSE,含测试);#4 限流桶上限加最久未活跃淘汰
         (middleware/ratelimit.go evictOldest,含 10200 键压测);#5 订单 ID 改 UUIDv7
         +锁内查重防覆盖(teller/uuid.go、telleralt/uuid.go + 两端 e2e 唯一性测试,
         消解表二#1);#6 warden 删除无比对用途的 SHA-256 指纹(warden.go 字段/赋值/
         函数全删,审计红线测试保持绿,消解「指纹可离线枚举」)
      2) 表二:#1、#2 被表一 #5、#1 消解(文档原对、代码错);#3 未配置 Provider
         口径改为「全挂载+501 降级」(architecture.md/en:145);#4 token 存储改为
         「四端已落、UE/Cocos 内存态未落仓」(architecture.md/en:238,对齐 layers.md);
         #5 App 门面改 CheckUpdate/CheckMaintenance/GetEnvironment(architecture.md/en:176)
      3) 表三:6 项只评估不实现——F77 订单持久化 / F78 热重载管理入口 / F79 多实例
         共享限流 / F80 UE/Cocos 令牌持久化新登记待议节(docs/todo.md/en,均标需用户拍板);
         consent 端到端=F31、审计可靠交付=F33,已有归属未重开
      测试:gateway 全量 `go test -race ./...` 30 包全绿;docs:build 绿。commit e39b328(后续台账回填行见 docs/todo.md 批次 95)。
- [x] 文档时效性清理（2026-10-18 用户令；2026-10-19 完成）：三路并行比对文档↔代码，
      23 处漂移全修（以代码为准，业务代码零改动；见 docs/todo.md 批次 97）。
      1) 测试数与目录结构：unity README(core 48→65、service 77→88 例,`dotnet test` 实测)、
         cocos/miniprogram 123→199 例、laybox 123→201 例(`node --test` 实测)、
         删去 unity 目录树里不存在的 `tests/` 节点
      2) 字节实测：unity core 77,851→79,759 B、TS core 44,448→44,570 B、
         UE 241,095→279,852 B（`du -sb`，口径注同步刷新至 2026-10-19）
      3) 网关描述：architecture.md/en 补 `audit/` 目录与 1MiB MaxBody（最外层）、
         HealthCheck 改为逐请求探活（无注册表剔除）、敏感端点列表按 account.go 改为
         register/login/guest/bind/refresh；layers.md/en middleware 枚举补 1MiB 请求体上限
      4) 契约面：events.md/en 删无发射方幽灵行 `app.maintenance_changed` / `app.force_update`
         （加更正注）、补 `branding.updated` 与 `realname.consent.updated`、
         信封形态加更正注指向 messages.md 的 SSE 帧；index.md/en realname→Frozen v1.1、
         payment→Frozen v1.1；realname.md/en consent 节标注冻结≠实现（F31）
      5) 状态文档：implementation-status.md/en 标题计数 1 待议/2 已代决 → 3 已代决
      6) 命令行示例六端逐条实测（gateway 构建 + 起服务后）：cocos/miniprogram/laybox/
         unity/ue/godot 全部跑出预期输出；examples/README.md godot 节补
         `COURIER_ENDPOINT`（默认 :8080 本机与 nginx 冲突，正是初次误判的根因）
      构建：docs:build 绿；未动版本号、未打 tag。
## 审核发现

- [x] 审核发现：F62 复核声称全仓 grep 仅命中评估/契约文档，但 `ops`/`live-ops` 实际还命中 `docs/roadmap.md:42`、`docs/sdk-selection.md:18` 等，检索证据不成立（docs/todo.md:18）。**已修正**：英文 F62 复核改为 `mail/gift-pack/redeem/promo` 仅命中评估/契约文档，`ops`/`live-ops` 另命中既有公告文案（roadmap.md:42、sdk-selection.md:18、architecture.md:255）但均非 F62 新需求；中文镜像同步。
- [x] 审核发现：本次只给英文 TODO 的 F62/F80 追加复核，中文镜像仍停留在旧文本，双语台账出现漂移（docs/todo.md:18, docs/todo.md:22；docs/zh/todo.md:18, docs/zh/todo.md:22）。**已修正**：中文 F62/F80 各补 2026-10-28 复核，与英文口径逐句对译。
- [x] 审核发现：F80 复核把 Laya 适配器并作 Cocos 持久化证据，且“Cocos ships only MemoryTokenStore”与实际的微信持久化适配器矛盾，Cocos 范围表述不准确（docs/todo.md:22；sdks/cocos/src/core/tokenStore.ts:12；sdks/cocos/src/platform/wechat/tokenStore.ts:9；sdks/laybox/src/platform/laya/tokenStore.ts:9）。**已修正**：英文 F80 复核改为 Cocos 含 MemoryTokenStore + 微信持久化适配器，Laya 适配器归属 laybox 端；中文镜像同步。
- [x] 审核发现：英文镜像 docs/todo.md 为 F62/F80 新增的 BLOCKED(2026-10-29) 注记整句以中文书写，与英文文档正文语言惯例及双语镜像逐句对译口径不符（docs/todo.md:18, docs/todo.md:22）。**已修正**：英文镜像 F62/F80 的 BLOCKED 注记改为英文整句（"missing operating-side requirement trigger (user decision)" / "missing UE engine integration surface (real machine/engine environment)"），与英文正文语言惯例及双语镜像逐句对译口径一致；中文镜像保持原句不动。测试：docs:build 绿。
