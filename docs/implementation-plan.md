# Donate 实现计划

> 面向执行者：按 executing-plans 连续完成；关键业务采用 test-driven-development，最终进行独立代码审查。用户已确认设计并授权独立推进，不另设任务间审批。

**目标：** 将定稿界面落为可发版的 Go 单程序，完成登录、支付、历史和管理。

**架构：** Go 模块化单体、SQLite 持久化、React 前端由 go:embed 提供；同源 Cookie 会话，支付和属地使用数据库任务。

**技术栈：** 当前最新稳定 Go、Node、pnpm、React、TypeScript、Vite；chi、modernc SQLite、OIDC、标准密码库。版本经官方发布接口 / registry 核验后锁定，安装使用 pnpm。

**规格：** [产品规则](requirements.md)、[后端设计](backend-design.md)、[支付](payment-integration.md)、[界面](design/README.md)、[属地](ip-geolocation.md)。

## 全局约束

- 所有实体 ID 为 UUID；金额整数分，1–10,000,000 分；未付有效 30 分钟。
- 登录必须，活跃无限续期、空闲 30 天到期；身份唯一、绑定不可解绑、封禁即时撤销。
- GET / POST 语义 API，业务通常 HTTP 200，基础鉴权 401 / 403；写操作 CSRF。
- 支付结果统一校验并持久化再 ACK；paid 不降级，物理删除不重建。
- 游客只看最新 10 条，公开数据最多省级，不泄露认证标识或完整 IP。
- 共用布局和基础组件，保持冻结设计视觉；生产不使用演示身份或内存业务数据。
- 不导入旧数据，不部署 / 推送 / 发布；真实渠道凭据仅由部署提供。

## 审查重点

- 多标签页续期与封禁 / 退出竞争：撤销不能复活，活动不能触发固定强退。
- 支付提交超时 / 重复通知 / 任务重启：同单号、幂等，不误入账。
- 删除与在途查询竞争：不复建，审计不留业务快照。
- 代理转发头 / 公开序列化 / 排序参数：不能越权泄漏或注入。
- 远端关单不支持或未知：本站到期仍失效，实际到账仍正确记录。

## 任务 1：可运行服务与存储基础

文件：`go.mod`、`cmd/donate/main.go`、`internal/{config,storage,observability}`、`migrations/`、`scripts/`。

- [ ] 先写临时 SQLite 的迁移 / 约束 / 重启测试与配置错误测试，运行 `go test ./internal/storage ./internal/config` 确认失败。
- [ ] 实现启动配置、UUID、短事务存储、迁移、日志轮转、实例锁、优雅退出、版本元数据。
- [ ] 同命令转绿；`go vet ./...`；以 `feat(core): 建立服务与持久化基础` 提交。

## 任务 2：认证、绑定与会话

文件：`internal/auth/{session,oauth,qq}.go` 及测试、`internal/user/`、HTTP 认证中间件。

- [ ] 先写活动超 30 天仍有效、空闲过期、封禁与续期竞争、流程 state 重放 / 绑定冲突测试，运行确认失败。
- [ ] 实现 `ResolveSession`、`RenewSession`、`FinishLogin`、`BindIdentity`，Cookie / CSRF、OIDC 发现验签与 QQ 内存头像检查；无生产模拟登录后门。
- [ ] `go test ./internal/auth ./internal/user` 转绿，身份流程使用可控测试上游；以 `feat(auth): 实现身份绑定和滚动会话` 提交。

## 任务 3：订单、支付与持久化任务

文件：`internal/order/`、`internal/payment/`、`internal/jobs/`、`internal/geo/` 及测试。

- [ ] 先写 `ApplyObservation` 并发幂等、金额 / 签名拒绝、晚到支付、删除竞争、租约过期与手动唤醒测试，确认失败。
- [ ] 实现 V2 / V1 适配器、受控跳转、通知 / 查单 / 关闭、创建幂等、30 秒扫描和既定退避；属地独立任务与故障转移。
- [ ] `go test ./internal/order ./internal/payment ./internal/jobs ./internal/geo` 转绿；以 `feat(payment): 实现支付通知与自动手动补单` 提交。

## 任务 4：API 与管理

文件：`api/openapi.yaml`、`internal/httpapi/`、`internal/admin/`、生成 DTO / 数据查询及测试。

- [ ] 先写越权、业务 200 / 鉴权 40x、游客十条、公开隐私、排序白名单、批量逐项状态 / CSV、审计删除测试，确认失败。
- [ ] 实现所有语义路由、限制 / 安全头、确认预览及幂等执行、概览 / 用户 / 审计、健康探针；以契约生成共享类型。
- [ ] `go test ./...` 和生成文件零漂移转绿；以 `feat(api): 提供订单和管理接口` 提交。

## 任务 5：正式前端与嵌入

文件：`web/src/{app,components,features,lib}`、`web/package.json`、pnpm 锁文件、`internal/webassets/`。

- [ ] 先写金额光标 / 限制、登录状态 / 401、列表权限及补单交互测试，确认失败。
- [ ] React 组件化迁移定稿 CSS / 布局和动画；共用导航 / 弹窗 / 账号 / 通知 / 日期 / 下拉组件，业务状态全部来自 API。
- [ ] 首页、管理四页、绑定、QQ 完成授权、深浅色 / 中英文 / 响应式；版本从 Go 注入；构建产物 embed。
- [ ] pnpm 格式 / lint / 类型 / 测试 / build，实际二进制 Playwright 验证转绿；以 `feat(web): 接入收据册和管理界面` 提交。

## 任务 6：CI、发版与运行说明

文件：`.github/workflows/{code-check,release}.yml`、依赖更新配置、`.env.example`、`README.md`、`docs/operations.md`。

- [ ] 工作流覆盖 Go 格式 / vet / race / govulncheck、前端完整检查、契约 / 查询生成、SQLite 迁移、实际 embed 二进制 E2E；只使用测试上游。
- [ ] 同提交发版、版本 / tag / commit 注入、三目标二进制及 SHA-256；不自动部署生产。实现一致性备份和恢复命令，记录部署配置和外部联调条件。
- [ ] 本机运行可运行的完整检查，记录平台受限项；独立整分支审查并修复关键发现，再以 `ci(release): 完善验证和版本发布` 提交。

实现过程中进度保存在被忽略的本计划账本，最终仓库只保留实际实现、有效测试、运行规范和这份可核对计划。
