# 运行说明

## 启动

下载 Linux amd64 二进制，将 `.env.example` 复制为当前目录的 `.env`。填写 `PUBLIC_URL`、`APP_SECRET`，按需配置登录和支付后运行 `./donate`。

`APP_SECRET` 使用 `openssl rand -base64 32` 生成，部署后保持不变。完整配置见 [`.env.example`](../.env.example)。

- 自动读取当前工作目录的 `.env`，进程环境变量优先；修改配置后重启。
- 默认生成 `donate.db`、`donate.log`、`donate.lock` 和 `backups/`；可用 `DATA_DIR` 修改保存位置，相对路径以工作目录为准。
- 默认监听 `127.0.0.1:8080`，单实例运行。旧部署更换数据位置前先停服、备份并迁移数据，或继续设置原 `DATA_DIR`。
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

控制台输出文本，`donate.log` 保存 JSON；`LOG_LEVEL` 支持 `DEBUG`、`INFO`、`WARN`、`ERROR`。日志按 50 MiB 轮转，最多保留 10 份、14 天。

## 访问统计

可选接入 Umami，在 `.env` 同时填写脚本地址与网站 ID：

```dotenv
UMAMI_SCRIPT_URL=https://stats.example.com/script.js
UMAMI_WEBSITE_ID=Umami 后台提供的网站 ID
```

重启后直接注入内嵌页面，无需重新构建；两项留空关闭。生产环境脚本地址使用 HTTPS。脚本与上报默认使用同一服务域名，CSP 自动放行该来源；排除页面 URL 查询参数、片段，并遵循浏览器 Do Not Track 设置。

## IP 属地

默认 `GEO_MODE=online`，无需下载库或配置 API Key。使用本地库时，先下载并解压支持 MaxMind City 字段结构的 `.mmdb` 文件，再修改 `.env`，例如：

```dotenv
GEO_MODE=local
GEO_DATABASE=./GeoLite2-City.mmdb
GEO_ONLINE_FALLBACK=true
```

- 示例文件放在启动程序的当前目录即可，也可填写其他相对路径或绝对路径；不要求固定目录，路径含空格时加引号。
- 修改配置、替换库文件后重启。程序不附带库、不自动下载或更新，也不热加载。
- `GEO_ONLINE_FALLBACK=true` 在本地查询报错、未命中或缺少省份时尝试内置在线服务，会把待查询的公网 IP 发给第三方；只有市、区县缺失不会触发补充。纯离线设为 `false`。库文件缺失或打不开会启动失败，不会降级为在线启动。
- 关闭查询设 `GEO_MODE=disabled`。前台最多展示省级，后台展示库里实际存在的字段；更新库不会重算历史订单。
- 已有订单（含迁移订单）可由管理员在订单详情或批量操作中明确选择“刷新属地”。刷新按当前模式绕过应用内 IP 缓存重新查询，取得国家及省份后替换快照，失败或缺省份则重试并保留原值，不改变支付状态；禁用模式不会排队。换库或重启不会自动刷新旧订单。

下载来源、服务商选择、格式限制及常见问题见 [IP 属地配置](ip-geolocation.md)。

## 备份与升级

自动备份在 `backups/`，保留最近 7 份，另行做好异机备份。手动备份和恢复前先停服务，并使用相同配置：

```sh
./donate --version
./donate backup
./donate restore backups/备份文件.db
```

恢复会撤销旧会话；完成后核对恢复点后的支付、封禁和删除操作。不要在运行时只复制 `donate.db`。

升级：停服 → 备份 → 替换二进制 → 启动并检查 `/health/ready`。回滚须使用旧版本兼容的备份。

## 发版

配置仓库 Secret RELEASE_TOKEN（仓库 Contents 读写权限）。在主分支手动运行 Prepare Release，选择 patch / minor / major，同步版本并推送标签；独立的 Release 由标签推送触发，检查、构建并发布。失败时重新运行对应的 Release，不自动部署。
