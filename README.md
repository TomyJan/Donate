# Donate

[donate.tomys.top](https://donate.tomys.top) 的打赏程序。Go + SQLite，内嵌 React 前端，支持 QQ / OIDC 登录、彩虹易支付和站点管理。

## 部署

从 [Releases](https://github.com/TomyJan/Donate/releases) 下载 Linux amd64 二进制和 `SHA256SUMS.txt`，在同一目录执行：

```sh
sha256sum --check SHA256SUMS.txt
mv donate-*-linux-amd64 donate
chmod +x donate
./donate
```

首次运行因缺少 `.env`，会生成 `.env.example` 并退出。执行 `cp .env.example .env`，填写站点地址、密钥和所需登录 / 支付配置，再运行 `./donate`。从当前工作目录读取配置，默认数据保存在 `data/`。

配置说明：[运行与备份、任务队列](docs/operations.md) · [登录与回调](docs/legal-and-login.md) · [支付接入](docs/payment-integration.md) · [IP 属地与本地库推荐](docs/ip-geolocation.md)。

## 开发

工具版本见 `go.mod`、`.node-version` 和 `package.json`。使用 pnpm：

```sh
pnpm install --frozen-lockfile
pnpm build
```

构建产物位于 `dist/`。本地开发在 `.env` 设置 `APP_ENV=development`、`PUBLIC_URL=http://127.0.0.1:5173`，分别运行 `go run ./cmd/donate` 和 `pnpm dev`。首次构建后再启动后端，以便嵌入页面资源。

开发、目录职责、生成文件与完整检查命令见[开发指南](docs/development.md)。`pnpm check` / `pnpm test` 只检查前端，不代替 Go 测试或 CI。

[文档索引](docs/README.md) · [产品规则](docs/requirements.md) · [架构](docs/backend-design.md) · [后续方向](docs/roadmap.md) · [API](api/openapi.json)

旧站位于 `legacy` 分支。许可证见 [LICENSE](LICENSE)，第三方资源见[授权说明](docs/design/vendor/README.md)。
