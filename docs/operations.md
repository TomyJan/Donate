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
- 关闭任务先持久化关闭本站入口并保留补单任务，再核验渠道；远端查询 / 关单失败至重试耗尽也不会恢复支付入口。V2 关单成功须收到验签后的 ACK，随后再次查单处理同时到账；查询失败保留持久化重试。V1 无通用关单能力时仅关闭本站入口。已关闭订单若实际到账仍可记为已支付，已支付永不降级。

上线前用实际商户确认 create / query / notify / close 的部署版本、响应签名、通道和限额，至少验证重复通知、通知丢失后补单及关闭后晚到账。本站查的是易支付账本，不能冒充直查支付宝 / 微信。普通测试没有使用真实凭据或真实扣款。

## IP 属地与日志

在线优先级为 IP9 → IPyou → I5P → 太平洋（IPv4）；单源 2 秒、总计 8 秒，带缓存、限速和熔断。免费接口可用性和授权可能变化，需在部署地核验。私网 / 保留地址不外发。`GEO_MODE=local` 支持 MaxMind 兼容 MMDB，配置 `GEO_DATABASE`；数据库自行获得合法授权并定期原子更新后重启。仅显式启用 `GEO_ONLINE_FALLBACK=true` 才允许本地缺失时在线补充。程序不附送或自动下载数据包，不保证任意 MMDB 的中国数据精度；数据选型见 [IP 属地方案](ip-geolocation.md)。

订单保存完整 IP，后台显示数据源能提供的细节；公开列表最多显示省级。属地失败不阻塞支付。控制台文本和 `DATA_DIR/logs/donate.log` 的 JSON 均保留中文正文及订单 / 用户 / 任务 / 请求 ID；默认 50 MiB 轮转、10 份、14 天、压缩。使用 `LOG_LEVEL=DEBUG|INFO|WARN|ERROR`，不记录原始 Cookie、OAuth token、商户密钥、留言或完整 IP。

## 收据归属、属地与手动请求历史

公开记录的 `is_mine` 由服务端有效会话与订单所有者比较计算，不接收客户端提供的所有者声明，也不向公开列表输出所有者用户 ID。未登录或无效会话始终为 `false`。公开列表仍然只包含已支付且未隐藏的订单；本人待支付、关闭或隐藏订单仅在已鉴权的“只看我的”范围出现。公开范围中本人的已支付记录也返回真实的 `is_mine=true`，以便展示本人详情按钮；本人可以读取自己被隐藏的留言，其他人及游客不能。详情和支付接口继续独立验证会话及所有权，不能仅凭列表标记授权。

记录列表与普通订单详情的 `province` 都来自现存 `order_locations.province`，只暴露省级，缺失时保持空字符串。普通详情不返回完整 IP、`location`、后台任务或手动请求历史；管理详情仍保留已有完整属地。`GEO_MODE=disabled`、私网 / 保留 IP、尚未查询成功或数据源未提供省级数据时，空省份表示无可用定位数据，不应凭 IP、昵称或环境猜测。E2E 上游仍禁用真实定位，为保留地址 `192.0.2.1` 显式写入北京 / 上海合成省份、来源 `fixture-synthetic`，仅验证渲染，不代表该 IP 的真实归属。

管理详情的 `jobs` 返回全部任务，不设任意条数上限；每条包含真实 `source` 与 `created_at`，不能将全部支付核验任务命名为“手动补单”。任务会合并同订单的活动工作，并可在自动 / 手动唤醒时更新来源，任务创建时间因此不能代表每次手动点击。新增的管理详情 `reconciliations` 按请求时间倒序列出每个成功、独立幂等键的手动补单请求：`id` 为现存请求记录的 UUID，`job_id` 为关联任务 UUID，`created_at` 为请求时间，`status/result` 为关联任务当前状态和结果。同键重试不新增记录，不同请求可以合并到同一任务，列表不宣称每条都进行了独立渠道查询。记录基于现存 `order.reconcile` 持久化幂等记录，并经当前任务及订单关联读取；当前实现不按幂等记录的过期标记删除历史，未来若引入清理必须先保留手动请求历史语义。物理删除订单会级联清理任务，历史立即不可读取，不保留或重建订单快照。

## 后台概览与审计

`GET /api/v1/admin/overview` 的 `range` 默认为 `30d`，可选 `today`、`yesterday`、`7d`、`30d`、`90d`、`all`、`custom`。当前实现采用以下统计口径，部署和产品验收应按此核对：

- 自然日使用 `Asia/Shanghai`，与服务器操作系统时区无关。`today` 为今天零点至明天零点；`yesterday` 为昨天零点至今天零点。`7d`、`30d`、`90d` 分别从今天向前数 6、29、89 个自然日的零点起，均截止明天零点，包含完整今天。
- 所有范围均左闭右开 `[from,to)`。`all` 没有日期边界；`custom` 必须同时提供 RFC3339 `from` / `to`（含时区），且 `from < to`。其他范围禁止提供 `from` / `to`。无效预设、缺失或无效日期、逆序或相同边界返回 `VALIDATION_FAILED`，不会静默改成默认范围。
- 收入、成功订单数、每日收入与渠道汇总使用当前 `paid` 订单的 `paid_at`。订单可在更早时间创建，只要支付时间在选定范围内就计入。每日点按北京时间分组，仅返回存在支付的日期，不补零；`90d` / `all` 不截断为最近 30 个点。
- 待支付和已关闭数量使用当前状态及 `created_at`，表示“在该范围创建且当前处于此状态的订单”，不是范围结束时的历史状态快照。
- 隐藏订单或隐藏留言仍计入全部后台统计；物理删除的订单不再计入。卡片、每日和渠道汇总在同一个数据库读快照中计算，金额和成功笔数保持一致。

后台订单列表的 `sort=message` 按留言排序，与其他排序字段一样仅映射固定 SQL 白名单，并用订单 ID 稳定打破并列值。

审计列表的 `target_type` / `target_id` 用于定位当前订单或用户；`target` 是可读摘要，不使用原始 ID。存在的订单返回当前昵称与金额摘要（例如 `Alice · ¥8.88`），并提供可选 `target_nickname`、`target_amount_fen`；用户目标返回当前昵称。关联用户或公开身份不存在时仍保留订单审计行，金额来自当前订单。目标不存在时 `target_exists=false`、`target=""`，不返回昵称或订单金额，前端禁用详情并自行翻译“已删除”。这些字段实时读取当前目标，不是审计发生时的快照；物理删除继续执行既有的关联任务清理，不保留删除前订单快照或金额。`actor` 在操作者身份不可用时使用稳定键 `system`，交由前端翻译。

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
