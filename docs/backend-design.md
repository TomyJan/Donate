# 服务架构

本文描述当前实现；产品约束见[产品规则](requirements.md)，开发入口见[开发指南](development.md)，部署参数见[运行说明](operations.md)。

## 模块与调用边界

单进程 Go 程序提供 API、内嵌 React 页面和 SQLite 持久任务，不依赖 Redis 或独立 worker。前端使用 React / TypeScript / Vite、Radix 和手写 CSS。

| 模块 | 职责 |
| --- | --- |
| `cmd/donate` | 校验配置、初始化日志与数据库、装配服务、启动 / 退出 |
| `httpapi` | chi 路由、请求校验、认证 / 权限 / CSRF、响应序列化 |
| `auth` | OAuth、身份绑定、QQ 号码核验、头像、会话与浏览器谱系 |
| `order` / `payment` | 订单规则、易支付协议与统一支付观察应用 |
| `jobs` | 领取、租约、调度、重试与执行历史 |
| `geo` | XDB / MMDB、在线适配、缓存、核验与安全更新 |
| `admin` | 管理查询、预览 / 执行、审计与任务恢复 |
| `storage` | SQLite、事务、Goose 迁移、sqlc 查询、备份与恢复 |
| `config` / `observability` / `webassets` | 配置 schema、slog 输出、嵌入页面与统计注入 |

业务模块位于 `internal/`。HTTP handler 不自行拼出支付成功状态；外部网络调用在数据库事务之外，最终写入再检查权限、目标和租约。

SQLite 使用 WAL、外键、busy timeout 和 `synchronous=FULL`，写事务由单写连接协调。实例锁防止多个进程同时运行。时间以 UTC 存储，界面和自然日统计使用 `Asia/Shanghai`，时间筛选左闭右开。

## 数据与一致性

内部用户、身份、会话、订单、任务及审计 ID 为 UUID，SQLite 使用 16 字节 BLOB，对外为标准 UUID 字符串。OAuth state 和会话凭据另用安全随机数。金额为整数分。

| 数据 | 主要约束 |
| --- | --- |
| `users` / `identities` | 身份稳定标识唯一，不按昵称、头像或 QQ 号合并账号 |
| `sessions` / `auth_flows` / `browser_families` | 仅存凭据哈希；流程一次性消费；谱系保存当前代际与失效水位 |
| `orders` / `order_locations` | 状态及支付流水受约束；属地为订单快照；旧记录允许无站内用户 |
| `payment_observations` | 保存经验证的渠道观察，不保存完整响应或通知 |
| `jobs` / `task_events` | 任务、执行历史与业务变更在同一事务提交 |
| `admin_operations` / `admin_operation_items` | 预览后逐项执行，保存实际结果 |
| `idempotency_records` / `task_retry_requests` | 调用者、动作、幂等键及请求摘要约束重复操作 |
| `audit_events` | 操作者、目标 ID、动作和结果，不保存已删除订单内容快照 |

实际结构见 `internal/storage/migrations/`，固定查询见 `internal/storage/queries.sql`。动态筛选参数化，排序只接受白名单；分页前排序，以 ID 保证稳定次序。

删除订单在事务中清理位置、支付观察、任务及历史等关联内容，审计只保留目标 ID 和删除结果。在途 worker 回写须验证目标仍存在及租约有效，不创建替代订单。隐藏订单、隐藏留言与物理删除是不同操作。

## 认证与前端会话

- OIDC 验证 issuer、audience、签名、有效期及 nonce；QQ 按应用范围 OpenID 或启用的 UnionID 识别。二者均校验 state 与浏览器绑定流程，OIDC 另用 PKCE S256。登录与绑定意图分离，绑定事务重新核对原会话。
- 会话有效期 30 天，仅剩余不超过 20 天且用户活跃时续至当前时间起 30 天。GET 不承担续期；普通续期不更换凭据。
- 每次受保护请求检查用户与会话状态；封禁撤销全部会话，解绑撤销对应身份的会话及绑定流程。解禁不恢复旧会话。
- Cookie 使用独立会话槽位，生产环境为 `__Host-donate_session_<session-id>`，带 Secure、HttpOnly、SameSite=Lax。非认证谱系锚点单独携带不能登录。
- 谱系的 current、alias 和发行序防止并发登录、退出、续期及迟到响应恢复旧账号。最新会话失效后不回退旧账号。不得手工清理水位；升级须单实例切换。
- 修改请求检查 Origin / Fetch Metadata 与 CSRF。GET 登录是显式浏览器导航入口，OAuth 回调按流程凭据验证；绑定仍为 POST + CSRF。
- 前端私有查询按完整会话 scope 隔离，切换后取消并移除旧缓存。数据 GET 传 `X-Session-Scope`，服务端在读取前比对实际会话；不匹配返回 403 / `CSRF_FAILED` 及 `X-Session-Mismatch: 1`，重新发现 profile，不清除已换新的会话。
- `site/info`、`user/profile`、`user/login/status` 是发现接口。缺少 scope 头的旧客户端仍兼容；该头不代替登录、管理员权限或 CSRF。

会话槽位 / 锚点的过量请求处理及旧 Cookie 迁移见[升级说明](operations.md#备份与升级)。头像兼容与登录设置见[登录文档](legal-and-login.md)。

## 支付状态与持久任务

创建订单同时写入属地与支付核验任务。支付开始再次检查所有权、封禁、有效期和状态；同一订单固定商户单号及渠道，超时不换号重发。

通知、自动查询和管理员补单生成经过验证的支付观察，通过同一事务应用。签名、商户、订单、金额、状态、渠道及流水唯一性必须满足约束；通知只在持久化成功或安全幂等重复后 ACK。

| 本站状态 | 经验证的结果 | 处理 |
| --- | --- | --- |
| pending / closed | 已支付 | 入账；closed 记录迟到支付 |
| paid | 相同到账 | 幂等确认，不重复计入或改写首次支付时间 |
| paid | 未付 / 未知 | 不降级，保存异常观察 |
| 任意未付 | 超时 / 查询不存在 | 未知结果，按策略继续核验 |
| 订单已删除 | 任意结果 | 不重建订单 |

本地过期或关闭不保证远端关单；浏览器回跳只唤醒核验，不证明到账。自动核验有 24 小时窗口；管理员可单笔补单，不提供强制设为已支付。

任务状态、租约、重试和管理恢复见[任务队列](task-queue-design.md)。属地查询及库更新使用独立任务，未就绪时的在线降级见[IP 属地](ip-geolocation.md)。协议入口与商户切换限制见[支付接入](payment-integration.md)。

## API、隐私与日志

Go DTO / 路由描述经 `cmd/contractgen` 生成 OpenAPI，再生成 TypeScript 类型；不是从 OpenAPI 生成 Go 业务模型。以 [API 契约](../api/openapi.json) 为字段参考。

API 使用 `/api/v1` 下的语义化 GET / POST。一般业务失败为 HTTP 200 + 稳定业务 code；未登录为 401，封禁 / 越权 / CSRF 为 403，基础设施故障使用相应 5xx。OAuth、支付通知、回跳及 CSV 按各自协议响应。

公开列表只返回已支付、未隐藏记录，游客最多最新 10 条。公开 DTO 不含完整 IP、QQ 或认证标识；本人详情允许查看本人订单 IP，管理员详情才提供完整可用属地与核验过程。管理员通过普通订单接口查看他人记录不会额外获得 IP。

日志正文和结构化字段都包含流程阶段、结果及关联 ID；不记录 Cookie、Token、授权参数、密钥、完整下载地址或第三方原文。运行日志与事务审计分开；日志故障标记就绪降级，不把进程存活当作全部健康。

配置、日志轮转、一致性备份及恢复见[运行说明](operations.md)。CI 的检查层次、生成链和构建入口见[开发指南](development.md)，真实渠道验证见[支付验收](payment-integration.md#上线核对)。
