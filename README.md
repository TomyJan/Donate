# Donate

[donate.tomys.top](https://donate.tomys.top) 的打赏程序。Go + SQLite，内嵌 React 前端，支持 QQ / OIDC 登录、彩虹易支付和站点管理。

## 部署

从 [Releases](https://github.com/TomyJan/Donate/releases) 下载二进制，复制 `.env.example` 为 `.env`，填写配置后运行：

```sh
./donate
```

Windows 运行 `donate.exe`。默认从当前工作目录读取 `.env`，数据库、日志和备份也保存在此。详见[运行说明](docs/operations.md)和[登录配置](docs/legal-and-login.md)。

## 开发

工具版本见 `go.mod`、`.node-version` 和 `package.json`。

```sh
pnpm install --frozen-lockfile
pnpm check
pnpm test
pnpm build
```

构建产物位于 `dist/`。本地开发在 `.env` 设置 `APP_ENV=development`、`PUBLIC_URL=http://127.0.0.1:5173`，分别运行 `go run ./cmd/donate` 和 `pnpm dev`。

接口生成：`pnpm generate`；检查：`pnpm check:generated`；浏览器测试：构建后运行 `pnpm test:e2e`。

[需求](docs/requirements.md) · [后端设计](docs/backend-design.md) · [界面设计](docs/design/README.md) · [API](api/openapi.json)

旧站位于 `legacy` 分支。许可证见 [LICENSE](LICENSE)，第三方资源见[授权说明](docs/design/vendor/README.md)。
