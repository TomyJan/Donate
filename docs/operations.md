# 运行说明

## 启动

下载 Linux amd64 二进制，将 `.env.example` 复制为当前目录的 `.env`。填写 `PUBLIC_URL`、`APP_SECRET`，按需配置登录和支付后运行 `./donate`。

`PUBLIC_URL` 必须是无路径、查询、片段或凭据的站点地址。CSRF 比较单独使用浏览器规范 origin：域名大小写、Unicode / IDN、IPv6 和默认端口会规范化，非默认端口保留；配置原串仍用于已注册的 OAuth / 支付回调，不会被此比较改写。普通 IPv4 使用完整点分十进制，缩写、八进制、十六进制等非规范数字地址在启动时拒绝；HTTP 仅允许开发环境。

`APP_SECRET` 使用 `openssl rand -base64 32` 生成，部署后保持不变。完整配置见 [`.env.example`](../.env.example)。

- `.env` 必须声明模板中每个键；可选值可留空，进程环境变量优先。缺文件、缺键、格式或读取权限错误时，写出最新 `.env.example` 后退出；不修改 `.env`，示例不含实际凭据。修改配置后重启。
- `APP_NAME=donate` 用作文件标识，仅限小写字母与单个连字符分段，最多 63 位。默认数据库 `data/donate.db`、锁 `data/donate.lock`、日志 `data/logs/`、备份 `data/backups/`；分别用 `DATA_DIR`、`LOG_DIR`、`BACKUP_DIR` 改目录，相对路径以工作目录为准。
- `DATA_DIR` 不影响日志、备份或 IP 库路径。更改 `APP_NAME` 会使用另一组数据库与文件，不自动迁移旧数据。
- 默认监听 `127.0.0.1:8080`，单实例运行。旧部署先停服备份，再移动数据库及配套文件，或显式保留原目录配置。
- 使用 HTTPS 反代，`PUBLIC_URL` 填对外站点地址。`REAL_IP_HEADER` 可填 `X-Real-IP` 等请求头，逗号分隔的 IP 取首项；反代须覆盖该头并限制源站直连。留空使用连接 IP。API、登录和支付回调不缓存，运行目录不要作为静态站点根目录。
- 首次登录后，从账号信息复制 ID，填入 `ADMIN_USER_IDS` 并重启即可获得管理权限。
- 健康检查：`GET /health/live`、`GET /health/ready`。

## 登录与支付

登录配置与回调地址见[登录配置](legal-and-login.md)。

易支付配置 `EPAY_URL`、`EPAY_PID`，默认 `EPAY_PROTOCOL=v2`，使用商户私钥和平台公钥；旧接口设为 `v1` 并配置 `EPAY_KEY`。密钥可通过对应的 `_FILE` 变量读取。

- 支付通知：`/api/v1/payment/notify`；浏览器回跳：`/api/v1/payment/return`。
- 额外支付跳转域名填入 `EPAY_REDIRECT_HOSTS`，逗号分隔，不支持通配符。
- 订单 30 分钟到期，24 小时内自动核验；管理员可在订单详情手动补单。

上线前用实际商户验证支付宝、微信的支付通知、漏单补单及关单。

## 日志

`LOG_OUTPUTS=console,log,jsonl` 默认开启全部输出，可用逗号组合，至少一项：`console` 为控制台文本，`log` 为文本文件，`jsonl` 为 JSON 文件。仅选 `console` 不创建或写入日志目录。

文件默认在 `data/logs/`，如 `donate_2026-11-01.log` / `.jsonl`。使用 `APP_NAME` 和运行地日期命名；每天新建，单卷 20 MiB，续卷追加 `.2`、`.3`，重启续写当日末卷。

`LOG_LEVEL` 支持 `DEBUG`、`INFO`、`WARN`、`ERROR`。`LOG_RETENTION_DAYS=30` 保留当日及前 29 天，启动和跨日写入时清理；0 不清理。只清理已启用格式、当前 `APP_NAME` 的日志。启用的文件输出初始化失败则拒绝启动；运行中写入失败会告警并标记就绪降级，其他输出继续。

失败日志包含流程阶段、可控原因、相关请求 / 用户 / 订单 / 任务标识及耗时，不记录 Token、Cookie、签名、完整下载地址或第三方响应正文。排查登录头像时，先保留一次重新登录的日志；仅有页面请求记录不能证明 OAuth 返回了什么。需要查看存量会话头像来源时临时设 `LOG_LEVEL=DEBUG`，复现后恢复 `INFO`。

## 访问统计

可选接入 Umami，在 `.env` 同时填写脚本地址与网站 ID：

```dotenv
UMAMI_SCRIPT_URL=https://stats.example.com/script.js
UMAMI_WEBSITE_ID=Umami 后台提供的网站 ID
```

重启后直接注入内嵌页面，无需重新构建；两项留空关闭。生产环境脚本地址使用 HTTPS。脚本与上报默认使用同一服务域名，CSP 自动放行该来源；排除页面 URL 查询参数、片段，并遵循浏览器 Do Not Track 设置。

## IP 属地

默认 `GEO_MODE=online`，无需下载库或配置 API Key。本地推荐 ip2region 的 XDB，可选配 City MMDB 回退：

```dotenv
GEO_MODE=local
GEO_XDB_IPV4=data/ipdb/ip2region_v4.xdb
GEO_XDB_IPV6=data/ipdb/ip2region_v6.xdb
GEO_DATABASE=data/ipdb/GeoLite2-City.mmdb
GEO_ONLINE_FALLBACK=true
```

- 默认文件在 `data/ipdb/`；可填写其他路径，含空格时加引号。不使用的库路径应留空。
- 按需填写 `GEO_XDB_IPV4_URL`、`GEO_XDB_IPV6_URL`、`GEO_DATABASE_URL`；`GEO_UPDATE_DAYS=7` 表示每 7 天更新。支持原始文件、gzip、tar.gz、zip，按内容识别，无需 URL 扩展名。MaxMind 选择 City MMDB 的 `suffix=tar.gz` 下载并提供认证，程序自动提取 `.mmdb`；[官方来源与配置示例](ip-geolocation.md#官方下载来源与示例)。验证后热重载，失败保留旧库；0 或空 URL 关闭对应下载。
- 本地按对应协议 XDB → MMDB 选择；MMDB 可留空，XDB 可只配一种协议。`GEO_ONLINE_FALLBACK=true` 允许就绪后的在线补充和省级核验。没有可用本地库时临时在线运行；配置了下载地址的缺失或损坏库立即入队，验证后自动恢复本地策略，不阻塞启动。`false` 不禁止未就绪期间的在线降级或显式库下载。
- 关闭查询设 `GEO_MODE=disabled`。前台最多展示省级，后台展示库里实际存在的字段；更新库不会重算历史订单。
- 已有订单可在订单详情或批量操作中“刷新属地”，绕过缓存。失败、缺省份或分歧未解决时重试并保留原属地，后台可展开查看最近一次核验依据；不改变支付状态。换库或重启不会自动刷新旧订单。

下载来源、服务商选择、格式限制及常见问题见 [IP 属地配置](ip-geolocation.md)。

## 任务队列

管理员进入“站点管理 → 任务”，可查看支付核验、关单、属地查询和属地库更新的状态、趋势及执行日志，按类型 / 状态 / 来源筛选并打开关联订单。无需 Redis 或独立 worker。库更新任务按 IPv4 XDB / IPv6 XDB / City MMDB 分开，不关联捐赠订单。

- 等待执行的任务按计划运行；未来计划时间、失败后的退避不代表队列卡住。执行中租约过期会由程序自动领取恢复。
- 失败任务可重试，待执行任务可提前执行；支持选中后批量操作。执行中和已完成任务不会重放，逐项结果会说明跳过或冲突原因。
- 任务页可见时每 5 秒自动刷新，保留筛选、分页和选中状态，不刷新整页。
- 执行历史从此功能升级后开始记录；旧任务只有当前快照，不补造过去日志，也不计入执行趋势。历史随数据库备份；物理删除订单时一并清除关联任务及执行记录。

## 备份与升级

自动备份默认在 `data/backups/`，启动迁移前及每运行 24 小时创建。`BACKUP_RETENTION_DAYS=7` 按快照文件名的 UTC 时间保留 7 天，0 不清理；只清理当前 `APP_NAME`，不限制份数。另行做好异机备份。手动备份和恢复前先停服务，并使用相同配置：

```sh
./donate --version
./donate backup
./donate restore data/backups/备份文件.db
```

恢复会撤销旧会话；完成后核对恢复点后的支付、封禁和删除操作。不要在运行时只复制 `donate.db`。

升级：停服 → 备份 → 替换二进制 → 对照新 `.env.example` 补齐所有键 → 启动并检查 `/health/ready`。回滚须使用旧版本兼容的备份。

会话 Cookie 独立槽位升级会自动回填存量会话及授权流程的浏览器谱系，旧固定名 Cookie 在有效请求中迁移；用户无需统一清除 Cookie。保持 `APP_SECRET` 不变，以校验非认证谱系锚点。此协议不支持新旧服务端长期混合处理同一浏览器 Cookie：升级时停旧实例再启动新实例，回滚使用兼容备份。不要手工清理 `browser_families` 的 current / alias 或重置发行序计数器；会话撤销、到期和每日记录清理后仍需保留水位，防止迟到响应恢复旧账号。

若一次请求携带超过 16 个认证槽位或 16 个锚点，服务端拒绝本次认证并返回 431，同时有界清理已观测的无效 / 已合并旧 Cookie。再次发起请求即可继续清理和重新校验；不会使用截断后的旧凭据认证。正常登录与续期会自动回收旧槽位，保留 30 日滑动有效期及 20 日续期窗口。

## 发版

配置仓库 Secret RELEASE_TOKEN（仓库 Contents 读写权限）。在主分支手动运行 Prepare Release，选择 patch / minor / major，同步版本并推送标签；独立的 Release 由标签推送触发，检查、构建并发布。失败时重新运行对应的 Release，不自动部署。
