# Donate

[donate.tomys.top](https://donate.tomys.top) 新版打赏站。Go 单实例、SQLite 持久化，React 前端内嵌在同一个二进制中，按版本发布。

支持 QQ / 自定义 OIDC 登录与绑定、活跃会话续期、彩虹易支付支付宝 / 微信、通知校验和自动 / 手动补单、本人历史、订单 / 用户 / 审计管理、选择导出和物理删除。首页保留收据册设计，适配手机、深浅色和中英文。

- [产品与实现约束](docs/requirements.md)
- [后端架构与协议](docs/backend-design.md)
- [部署、配置、备份恢复及发版](docs/operations.md)
- [生成的 OpenAPI 契约](api/openapi.json)
- [彩虹易支付接入方案](docs/payment-integration.md)
- [最终界面、组件和交互规范](docs/design/README.md)
- [IP 属地数据方案](docs/ip-geolocation.md)
- [用户协议、隐私提示与登录配置](docs/legal-and-login.md)
- [首页 HTML](docs/design/preview/index.html) / [后台 HTML](docs/design/preview/admin.html)

构建正式程序（工具版本见 `.node-version`、`go.mod`、`package.json`）：

```sh
pnpm install --frozen-lockfile
pnpm check
pnpm test
pnpm build
```

输出在 `dist/`。参照 `.env.example` 将配置载入进程环境后运行二进制；程序不会自行读取 `.env`。部署只需二进制与持久化数据目录。真实 OAuth 应用和商户凭据由部署提供，上线前仍须完成实际渠道联调及 QQ 头像检查校准；普通测试使用独立上游，没有生产模拟登录或付款入口。旧数据暂未导入。

主要代码在 `cmd/donate`、`internal/`、`web/src`，数据库迁移位于 `internal/storage/migrations`。执行 `pnpm generate` 更新 Go DTO 推导的 OpenAPI、TypeScript 类型与 sqlc 固定查询代码，`pnpm check:generated` 检查漂移；`pnpm test:e2e` 对已构建的真实二进制运行浏览器测试。CI 还检查 Go vet / race、依赖风险和 Linux / Windows 路径行为。

冻结设计参考页面仍可独立运行（Python 3.10+）：

```sh
python docs/design/serve.py
```

打开 <http://127.0.0.1:52771/> 与 <http://127.0.0.1:52771/admin>。也可直接打开两份 HTML；推荐本地服务以获得跨页演示状态、正确路由和当前 Git 版本链接。默认使用管理员示例身份，登录、支付和管理操作均为本地演示。

编辑 `docs/design/src/` 后生成并检查：

```sh
python docs/design/build.py
python docs/design/build.py --check
```

旧站 PHP、运行说明与易支付逻辑参考保留在独立的 `legacy` 分支。`docs/design` 是最终视觉基准，正式业务使用 React 共用组件与 Go 接口。

项目许可证见 [LICENSE](LICENSE)，图标授权见 [第三方资源说明](docs/design/vendor/README.md)。
