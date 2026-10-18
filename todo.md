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
      测试:gateway 全量 `go test -race ./...` 30 包全绿;docs:build 绿。commit 后回填 hash。
