# 开发指南

## 环境与启动

Go、Node 和 pnpm 版本分别见 [`go.mod`](../go.mod)、[`.node-version`](../.node-version) 和 [`package.json`](../package.json)。Python 3 仅用于静态设计参考的生成与检查。

```sh
pnpm install --frozen-lockfile
cp .env.example .env
pnpm build
```

开发配置设 `APP_ENV=development`、`PUBLIC_URL=http://127.0.0.1:5173`，保留模板所有键。分别在两个终端运行：

```sh
go run ./cmd/donate
pnpm dev
```

Vite 的 `/api`、`/health` 代理到 `127.0.0.1:8080`；修改后端监听端口时同步修改 [`web/vite.config.ts`](../web/vite.config.ts)。浏览器打开 5173，修改 Go 代码后重启后端。开发也使用真实 SQLite 和配置的外部服务，勿指向生产数据。

## 代码入口

| 目录 | 职责 |
| --- | --- |
| `cmd/donate` | 配置、日志、数据库和服务启动装配；备份 / 恢复命令 |
| `internal/httpapi` | HTTP 路由、鉴权、输入输出与错误映射 |
| `internal/auth` | QQ / OIDC、身份绑定、会话和头像校验 |
| `internal/order`、`internal/payment` | 订单规则、支付适配与到账观察 |
| `internal/jobs`、`internal/geo` | 持久任务、属地查询及库更新 |
| `internal/admin` | 管理查询、批量操作、审计与任务管理 |
| `internal/storage` | 事务、迁移、查询、备份与恢复 |
| `internal/config`、`internal/observability`、`internal/webassets` | 配置 schema、日志、内嵌页面与 Umami |
| `web/src/app` | 全站布局、会话和请求作用域 |
| `web/src/features`、`web/src/components` | 页面业务及前后台共用组件 |
| `web/src/lib`、`web/src/styles` | 客户端工具、翻译、共享样式 |
| `scripts` | 构建、生成、E2E fixture 和版本发布工具 |

前端使用 React、Radix、TanStack Query、Motion 和手写 CSS。源码保持可读排版，生产构建由 Vite 压缩。第三方 vendor 不手动重排。

## 生成文件

```sh
pnpm generate
pnpm check:generated
```

- 配置 schema `internal/config/settings.go` → `.env.example`。
- Go DTO / 路由描述 → `cmd/contractgen` → `api/openapi.json` → `web/src/generated/api.ts`。
- `internal/storage/queries.sql` 与迁移 → sqlc → `internal/storage/dbgen`；具体输入见 [`sqlc.yaml`](../sqlc.yaml)。
- `pnpm --dir web build` → `internal/webassets/dist/`，再由 Go 嵌入；该目录的构建产物不提交。
- `python docs/design/build.py` → `docs/design/preview/*.html`，这两份视觉基准需要提交。

修改来源后重新生成并审查差异，不手改生成文件。`check:generated` 会实际重新生成，发现漂移时会留下待检查的文件差异。

## 验证

按改动范围执行；完整 CI 以 [code-check.yml](../.github/workflows/code-check.yml) 为准。

```sh
# 前端格式、lint、类型及单元 / 组件测试
pnpm check
pnpm test
# 构建和版本工具
pnpm test:tooling
# Go 静态检查与测试（先构建过前端资源）
go vet ./...
go test -count=1 ./...
go mod tidy -diff
# 生成文件与设计参考
pnpm check:generated
python docs/design/build.py --check
```

用 `gofmt -l cmd internal` 检查 Go 格式，输出应为空。Linux CI 另运行竞态检测、关键函数覆盖门禁和漏洞检查：

```sh
go test -race -count=1 -coverprofile coverage.out ./...
go run ./cmd/coveragecheck coverage.out
```

浏览器回归先安装 Chromium 并构建当前程序：

```sh
pnpm --dir web exec playwright install chromium
pnpm build
pnpm test:e2e
```

Linux 缺少浏览器系统依赖时安装用 `--with-deps chromium`。E2E 在 18080 启动隔离测试服务，使用临时数据库和模拟 OAuth / 支付服务；不要同时运行多个实例，或把手动预览服务放到该端口。更改生产代码后重新构建，避免测试旧二进制。

认证、支付、任务或数据库变更需覆盖权限、并发、幂等与失败恢复；纯文档改动检查链接和实现一致性即可。外部商户、身份提供方及 IP 数据覆盖需单独联调，隔离测试不能证明其生产可用性。

## 构建与发布

`pnpm build` 先构建前端，再构建当前平台的 `dist/donate`（Windows 为 `.exe`）并写校验和。构建含 Git 版本信息，需在 Git 仓库内执行。

Prepare Release 同步版本并推送标签；独立 Release 工作流检查同一提交，仅发布 Linux amd64 二进制和 `SHA256SUMS.txt`。配置与操作见[运行说明](operations.md#发版)。提交、推送和发布是独立动作。
