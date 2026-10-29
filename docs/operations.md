# 运行与维护

## 构建与本机验证

依赖锁定在 `go.mod`、`.node-version`、根 `package.json` 和 `pnpm-lock.yaml`。本次建立工程时核验的稳定版本为 Go 1.27.3、Node 26.13.0、pnpm 12.16.1。依赖升级由 Dependabot 提交后经过同一套检查，不在构建时浮动升级。

```sh
pnpm install --frozen-lockfile
pnpm check
pnpm test
pnpm test:tooling
pnpm check:generated
pnpm build
go vet ./...
go test ./...
pnpm --dir web exec playwright install chromium
pnpm test:e2e
```

`pnpm build` 先构建 React，再把静态资源嵌入 Go，输出 `dist/donate`（Windows 为 `dist/donate.exe`）及校验和。运行二进制不依赖 Node、pnpm 或前端目录。开发可分别运行 `go run ./cmd/donate` 与 `pnpm dev`；Vite 将 `/api`、`/health` 转到 `127.0.0.1:8080`，此时 `PUBLIC_URL` 应与浏览器访问的 Vite origin 完全相同。

仅查看空站可设 `APP_ENV=development`、`PUBLIC_URL=http://127.0.0.1:8080` 后启动。未配置登录提供方就不会出现可用授权入口，未配置易支付不能创建支付订单；没有模拟管理员或模拟付款后门。浏览器测试使用独立、带 `e2e` 构建标签的临时上游，生产二进制没有这个入口。

## 单实例部署

1. 把二进制放在 `/opt/donate/donate`，数据目录用独立磁盘目录，如 `/var/lib/donate`；只运行一个进程，不把 SQLite 放到网络共享文件系统。
2. 参照 `.env.example` 建立 `/etc/donate.env`，权限 `0600`。程序读取进程环境，**不自动加载 `.env`**。生产必须配置长期稳定的 `APP_SECRET`，可用 `openssl rand -base64 32` 生成。
3. 用 HTTPS 反代到 `127.0.0.1:8080`，设置真实 `PUBLIC_URL`，只把实际反代地址加入 `TRUSTED_PROXY_CIDRS`。不要把所有来源设为可信代理，也不要缓存 API、OAuth 或支付回调。
4. 首次正常登录，在账号信息取得站内 ID，加入 `ADMIN_USER_IDS` 并重启；头像菜单随后显示站点管理。没有默认管理员账号。

systemd 示例（先创建有权读配置 / 密钥、写数据目录的 `donate` 用户）：

```ini
[Unit]
Description=Donate
After=network-online.target
Wants=network-online.target

[Service]
User=donate
Group=donate
WorkingDirectory=/opt/donate
EnvironmentFile=/etc/donate.env
ExecStart=/opt/donate/donate
Restart=on-failure
RestartSec=5
TimeoutStopSec=30
UMask=0077
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/donate

[Install]
WantedBy=multi-user.target
```

反代至少转发 `Host`、`X-Forwarded-For` 和 `X-Forwarded-Proto`；可信链只用来确定来源 IP，站点 origin 始终以 `PUBLIC_URL` 为准。不要把 `DATA_DIR`、环境文件或密钥目录设置为反代静态根目录。`GET /health/live` 检查进程，`GET /health/ready` 检查数据库和日志降级状态；后者异常返回 503。

## 身份提供方

在提供方登记以下精确 HTTPS 回调，不附带通配符：

- OIDC：`https://站点域名/api/v1/user/oauth/callback/oidc`；需允许 authorization code、PKCE S256，返回 `openid profile` 对应的标准身份和头像 / 昵称声明。
- QQ：`https://站点域名/api/v1/user/oauth/callback/qq`；应用需要 QQ 互联登录权限。首次登录或绑定后完成 QQ 号检查。

身份键为提供方 issuer / subject（QQ 为 OpenID），QQ 号码与头像匹配只辅助确认展示资料，不能代替 OAuth 身份。图片仅在内存比较，不保存到磁盘。私有参考仓库当前无法直接读取，正式上线前必须用真实 QQ 样本校准头像相似度并验证互联应用回调；不能把同头像当作强身份证明。绑定不切换本次头像 / 昵称，不能解绑或合并已有独立账号。

会话空闲 30 天失效，活跃时持续续期，没有固定总时长。所有标签页共用原随机凭证；封禁或退出即撤销，解禁不会恢复旧凭证。

## 彩虹易支付

默认 V2 RSA（至少 2048 位），明确配置为 `v1` 时使用 MD5 兼容。密钥从权限受限文件读取，平台公钥与商户私钥不要互换。`EPAY_URL` 是固定可信网关根地址。若跳转支付页需要其他域名，把精确域名加入 `EPAY_REDIRECT_HOSTS`，不支持通配符或任意外部跳转。

- 通知：`/api/v1/payment/notify`，接受协议规定的 GET 或表单 POST；仅在验签、商户 / 单号 / 金额 / 渠道核对并提交数据库后返回 `success`。
- 回跳：`/api/v1/payment/return`，只唤醒查单并回首页，不能据此入账。
- 本站订单 30 分钟到期；自动核验计划为创建后约 1、3、8、18、30 分钟及 1、2、6、12、24 小时。失败约 1、2、5、10 分钟退避加抖动，尊重 Retry-After 和商户熔断。管理员可在单笔详情手动补单。
- V2 关单成功须收到验签后的 ACK；V1 无通用关单能力时仅关闭本站入口。已关闭订单若实际到账仍可记为已支付，已支付永不降级。

上线前用实际商户确认 create / query / notify / close 的部署版本、响应签名、通道和限额，至少验证重复通知、通知丢失后补单及关闭后晚到账。本站查的是易支付账本，不能冒充直查支付宝 / 微信。普通测试没有使用真实凭据或真实扣款。

## IP 属地与日志

在线优先级为 IP9 → IPyou → I5P → 太平洋（IPv4）；单源 2 秒、总计 8 秒，带缓存、限速和熔断。免费接口可用性和授权可能变化，需在部署地核验。私网 / 保留地址不外发。`GEO_MODE=local` 支持 MaxMind 兼容 MMDB，配置 `GEO_DATABASE`；数据库自行获得合法授权并定期原子更新后重启。仅显式启用 `GEO_ONLINE_FALLBACK=true` 才允许本地缺失时在线补充。程序不附送或自动下载数据包，不保证任意 MMDB 的中国数据精度；数据选型见 [IP 属地方案](ip-geolocation.md)。

订单保存完整 IP，后台显示数据源能提供的细节；公开列表最多显示省级。属地失败不阻塞支付。控制台文本和 `DATA_DIR/logs/donate.log` 的 JSON 均保留中文正文及订单 / 用户 / 任务 / 请求 ID；默认 50 MiB 轮转、10 份、14 天、压缩。使用 `LOG_LEVEL=DEBUG|INFO|WARN|ERROR`，不记录原始 Cookie、OAuth token、商户密钥、留言或完整 IP。

## 备份、恢复与升级

每次打开已有数据库前及运行中每 24 小时创建 SQLite `VACUUM INTO` 一致性快照，默认保留最近 7 份，位置 `DATA_DIR/backups`。部署方另做异机备份。不要在 WAL 活跃时只复制 `donate.db`。

手动维护命令与服务共用实例锁，必须先停服务，使用相同运行用户及环境：

```sh
donate --version
donate backup
donate restore /绝对路径/已确认的备份.db
```

恢复先校验完整性、外键和迁移版本；只接受当前二进制兼容的备份。替换前保存当前一致性快照，源备份不变，恢复撤销所有旧会话 / 授权流程，清理在途任务租约。恢复会回到旧时间点，因此还需人工核对之后的封禁、物理删除与支付变化，再重新登录并按需补单；在线删除不意味着旧备份已同步抹除。

升级：停服务 → 额外备份并异机保存 → 替换二进制 → 启动迁移 → 检查 ready、版本和关键页面。迁移失败不会开放业务；不能直接用旧二进制写入新版数据库，回滚须使用与旧版匹配的备份。旧 PHP 数据导入独立安排，本版本没有自动导入。

## CI 与发版

关键函数覆盖门禁固定为每项至少 80%：会话 Resolve / Renew / Logout、授权 claimFlow、订单 Create / Apply、支付 verify、任务 Claim / Finish。函数缺失也拒绝通过，不使用全仓平均数稀释关键路径；其余业务结合单元测试与真实二进制 E2E 验证。

`code-check.yml` 覆盖格式 / 类型 / lint、前后端单测、Go vet / race、依赖风险、生成物漂移、Linux / Windows SQLite 测试及真实嵌入二进制的浏览器验证。SQLite 首个版本仅有初始迁移；后续升级必须增加从已发版快照迁移的回归测试。

正式发布只从默认分支手动运行 Release，输入 `x.y.z`。工作流固定完整 SHA，先通过同提交检查，再建立本地精确 tag 构建 Linux amd64 / arm64、Windows amd64，验证校验和和发布二进制的浏览器行为，最后才推 tag、创建 GitHub Release。不会自动部署生产，分支每次更新也不会自动发版。若发布步骤中断且远端 tag 已存在，保留已验证的产物，核对 SHA 后人工恢复发布，禁止覆盖已有版本资产。

`pnpm build --release --version=x.y.z` 要求干净工作区和匹配的精确 tag。精确 tag 构建页脚显示 `Vx.y.z` 并链接 tag；其他构建显示 `Vx.y.z-短提交` 并将整个版本链接到完整 commit。
