# IP 属地配置

## 选择模式

| 配置 | 行为 |
| --- | --- |
| `GEO_MODE=online` | 默认值，使用内置在线查询与故障转移，无需下载数据或填写 API Key |
| `GEO_MODE=local` | 优先 XDB，缺失时回退可选 MMDB；仅开启 `GEO_ONLINE_FALLBACK` 时允许在线核验与补充 |
| `GEO_MODE=disabled` | 不查询属地，仍记录下单 IP |

## 本地库怎么配

1. 推荐从 [ip2region 官方仓库](https://github.com/lionsoul2014/ip2region/tree/master/data) 下载 `ip2region_v4.xdb` 和 `ip2region_v6.xdb`，确认实际下载到二进制文件，不能保存成 GitHub 的 HTML 页面。也可继续使用下面推荐的 City MMDB。
2. 放在程序可读的位置，修改启动工作目录里的 `.env`，例如：

```dotenv
GEO_MODE=local
GEO_XDB_IPV4=data/ipdb/ip2region_v4.xdb
GEO_XDB_IPV6=data/ipdb/ip2region_v6.xdb
GEO_DATABASE=data/ipdb/GeoLite2-City.mmdb
GEO_ONLINE_FALLBACK=true
```

3. 重启程序。后台订单详情可查看下单 IP、采用的属地与来源，以及可展开的“属地核验”记录；`xdb` / `mmdb` 是本地来源。上例的 MMDB 可选，不使用时将 `GEO_DATABASE` 留空；IPv4 / IPv6 的 XDB 也可只配一种。

默认库目录为 `data/ipdb/`，也可使用其他路径。相对路径以**启动工作目录**为准，不是二进制目录或 `DATA_DIR`；路径含空格时加引号。程序不附带本地库，可自行放置或启用自动下载。

本地顺序为 **对应协议的 XDB → MMDB**：XDB 未命中、查询失败或缺省份时继续 MMDB；另一协议未配置 XDB 时也可使用 MMDB 或允许的在线补充。启用对应下载时，首次缺失会尝试下载；未启用、下载失败、现有文件损坏或协议不匹配则启动失败。纯本地模式至少配置一个库；不使用的路径应留空。

XDB 支持官方 v3 数据的 `国家|省份|城市|ISP|国家两字母代码`，以及旧 v2 IPv4 数据的 `国家|区域|省份|城市|ISP`。`0`、`未知` 等占位符不算有效省份；自定义字段布局不能直接使用。MMDB 仍按 **MaxMind City 字段结构**解析：`country.names`、`subdivisions[].names`、`city.names` 等，名称优先 `zh-CN`，缺失时取 `en`。不是所有 `.mmdb` 都兼容，Country / ASN 库不能当 City 库使用。

省市、区县、坐标、ISP / ASN 仅保存选定来源实际提供的字段，不将不同来源的省、市、区县拼成地址。XDB 不提供坐标时，也不会把其他来源的坐标附到它的结果上。

## 数据源推荐

### 匹配当前解析器

| 数据源 | 选择建议 | 下载与注意事项 |
| --- | --- | --- |
| [ip2region XDB](https://github.com/lionsoul2014/ip2region) | 推荐先试的免费本地来源，IPv4 / IPv6 分开下载 | 选择官方 data 目录中的两个 XDB；随项目更新数据，实际覆盖和精度须用已知位置样本验证 |
| [MaxMind GeoLite2 City](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data/) | 免费入门，先验证本站实际省市显示是否够用 | 注册账号，从账户下载城市级 MMDB；需遵守 GeoLite 署名及更新要求 |
| [MaxMind GeoIP City](https://dev.maxmind.com/geoip/docs/databases/city-and-country/) | 付费候选，与现有解析器同一字段体系 | 选择 `GeoIP2-City.mmdb`，订阅更新；不要误选独立 Country / ASN / ISP 产品 |
| [DB-IP IP to City Lite](https://db-ip.com/db/download/ip-to-city-lite) | 免费替代，适合先比较大陆 IPv4 / IPv6 样本 | 下载页选择 MMDB；`.mmdb.gz` 可交给程序自动解压，手动放置时先解压；按月更新，展示结果须按其要求署名并链接 DB-IP |
| [DB-IP IP to Location](https://db-ip.com/db/ip-to-location) | 付费候选，可与 MaxMind 比较省市与第二级行政区覆盖 | 选择 MMDB，按订阅获取更新；不是在线 API 套餐 |

两家数据的字段结构分别见 [MaxMind City](https://dev.maxmind.com/geoip/docs/databases/city-and-country/)、[DB-IP Lite](https://db-ip.com/db/format/ip-to-city-lite/mmdb.html) 和 [DB-IP Location](https://db-ip.com/db/format/ip-to-location/mmdb.html)。这表示字段可接入，不代表已验证所有中国 IP 的精度；正式选型用已知位置的宽带、移动网络、校园网及 IPv6 样本比较。

免费库也有使用条件：[GeoLite 对外展示要求署名](https://support.maxmind.com/knowledge-base/articles/sell-or-display-data-from-geolite-databases-and-web-services)，[DB-IP Lite 下载页](https://db-ip.com/db/download/ip-to-city-lite)列明页面署名要求。本程序不会根据所选库自动添加来源署名，部署方需按所用数据的要求补充。

### 中国区县数据候选，当前需要另行适配

| 数据源 | 适用方向 | 当前限制 |
| --- | --- | --- |
| [IPIP.NET](https://www.ipip.net/product/ipv6.html) | 中国城市 / 区县、运营商等，按产品选购 IPv4 / IPv6 数据 | 官方 IPDB 不能直接填入 `GEO_DATABASE`，需增加解析适配；公开展示结果的授权需向供应商确认 |
| [纯真商业离线库](https://www.cz88.net/) | 中国省市区县数据候选，可向供应商索取样本比较 | 需适配交付格式及字段，不能仅填路径启用 |

先试 XDB，按需搭配 City MMDB；如果仍需要更好的中国区县覆盖，再向国内服务商要样本。查询引擎开源不代表所有第三方数据包都可任意使用，更新周期与公开展示授权需按具体数据源核对。不要因为“支持中文”就假定每个地址都有省、市、区县。

## 更新与验证

在 `.env` 填写需要的库下载地址：

```dotenv
GEO_UPDATE_DAYS=7
GEO_XDB_IPV4_URL=
GEO_XDB_IPV6_URL=
GEO_DATABASE_URL=
```

- 地址由供应商提供，也可使用自己的代理；生产仅 HTTPS，可包含查询密钥或 URL 账号密码。修改配置后重启，凭据不要公开。各来源示例见下文。
- 仅 `GEO_MODE=local` 启用下载。`GEO_UPDATE_DAYS=0` 关闭，URL 为空则该库不下载；对应文件路径不能留空。`GEO_ONLINE_FALLBACK=false` 不禁止这些显式配置的下载。
- 启动补下载缺失文件；已有库按文件修改时间判断，每小时检查是否达到配置天数。失败保留旧库，下次检查重试。
- 按文件内容识别，URL 不要求有扩展名。支持原始 XDB / MMDB、gzip（如 `.mmdb.gz`）、tar.gz / tgz、带 `ustar` 头的 tar、zip；不支持 CSV、xz、bz2、7z、rar。下载与解压总量各限 512 MiB、最多 256 项，压缩包只能有一个匹配库；MMDB 包内匹配 `.mmdb`，XDB 包内匹配对应协议的 `.xdb`。下载地址应返回文件，不能是网页。
- 同目录暂存、验证格式及协议后替换并热重载，旧缓存同时失效；安装失败回滚旧文件。程序须有库目录写权限。手动换库仍应停服后替换并重启，不原地覆盖正在读取的文件。
- 更新库不重算旧订单。管理员可单笔或批量“刷新属地”，绕过缓存；取得可接受省份后替换快照。失败、缺省份或分歧未解决时保留原值，失败后分别等 1 分钟、10 分钟，最多执行 3 次。禁用模式不可刷新，重复操作共享活动任务，不改变支付状态。

### 官方下载来源与示例

**ip2region**：官方 [data 目录](https://github.com/lionsoul2014/ip2region/tree/master/data) 提供两种协议的原始 XDB，可直接配置以下地址。更新跟随仓库数据，不保证每天发布。

```dotenv
GEO_XDB_IPV4_URL=https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ip2region_v4.xdb
GEO_XDB_IPV6_URL=https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ip2region_v6.xdb
```

**MaxMind**：登录账户的 [Download Databases](https://www.maxmind.com/en/accounts/current/geoip/downloads)，选择 GeoLite2 City 或已订阅的 GeoIP City，复制 **MMDB 下载永久链接**。官方通常提供 `tar.gz`，不是裸 `.mmdb`；[下载说明](https://dev.maxmind.com/geoip/updating-databases/)要求用账户 ID 和 License Key 做 HTTP Basic 认证，并跟随 HTTPS 重定向。

本程序可把认证信息写在下载 URL 中，例如：

```dotenv
GEO_DATABASE=data/ipdb/GeoLite2-City.mmdb
GEO_DATABASE_URL="https://YOUR_ACCOUNT_ID:YOUR_LICENSE_KEY@download.maxmind.com/geoip/databases/GeoLite2-City/download?suffix=tar.gz"
```

替换两个凭据占位符；用户名或密钥若含 URL 保留字符，先进行百分号编码。付费库以账户实际提供的永久链接为准。`suffix=tar.gz` 是 MaxMind 的接口参数，不是本程序的配置项：程序会从包内目录提取唯一的 `.mmdb`，保存为 `GEO_DATABASE` 指定的文件，无需手动解压。不要选择 `*-CSV` 或 `suffix=zip` 的 CSV 下载。网络需允许官方重定向后的下载域名。

仅填永久链接而不提供认证，通常会返回 401。也可配置自有的 HTTPS 认证代理，或用 [geoipupdate](https://dev.maxmind.com/geoip/updating-databases/) 下载，在停服时放入本地库路径。不要公开含凭据的 URL。

**DB-IP**：从 [IP to City Lite 下载页](https://db-ip.com/db/download/ip-to-city-lite)复制 MMDB 下载链接，通常为 `dbip-city-lite-YYYY-MM.mmdb.gz`，可直接填入 `GEO_DATABASE_URL`；本地路径仍用 `.mmdb`。免费链接包含年月，每月需更换，自动检查不会替你改 URL；付费库使用订阅提供的稳定下载地址。

IPIP.NET 的 IPDB、纯真等其他格式仍需适配，不能仅改扩展名后配置。

### 检查本地文件

- 可安装 `mmdblookup` 验证文件及字段，例如：

```sh
mmdblookup --file data/ipdb/GeoLite2-City.mmdb --ip 223.5.5.5
```

示例 IP 仅用于检查读取是否正常，不作为准确率样本。最终用自己的公网 IPv4 / IPv6 和已知实际位置核对；本机 `127.0.0.1` 或内网地址不查询属地。经反代部署时先核对 `REAL_IP_HEADER`，反代应覆盖该头并限制源站直连，避免把反代 IP 误当访客 IP。

## 在线查询与补充

内置顺序为：

- IPv4：**IP9 → IP优 → I5P → 太平洋**。
- IPv6：**IP9 → IP优 → I5P**，不向太平洋发送 IPv6。

查询通过异步任务执行，有缓存、请求合并、限流、超时、熔断和失败切换，不阻塞支付。online 模式沿用首个有国家及省份的完整在线结果；只有国家时继续回退，所有来源都缺省份则保留部分信息。当前不支持自定义在线服务商 URL / Key。

本地模式设置 `GEO_ONLINE_FALLBACK=true` 时：

- 本地无有效省份：按上述顺序回退在线来源，采用首个完整结果。
- 本地已有省份：额外取得一个完整在线结果核验。省份一致就保留本地完整结果，不为补齐市、区县继续请求。
- 第一在线来源与本地冲突：再取一个完整在线结果；它与本地一致就保留本地，与前一在线结果一致则采用前一在线完整结果，并保留分歧标记。最多取得两个完整在线候选，失败或缺省份的来源继续按原顺序切换。
- 仍有分歧或无法核验：不猜省份，也不覆盖旧有效快照。任务保留诊断并重试。全部在线来源不可用但没有实际冲突时，本地完整结果仍可使用，标记为“单一来源”。

国家和中国省份的常见别名会统一，例如 China / 中国、广东省 / 广东。多来源一致仅表示本次核验一致，不是准确率保证；多个接口可能共享上游数据。完整结果通常缓存 24 小时，部分结果、分歧、失败以及联网核验暂不可用的本地结果缓存 5 分钟；手动刷新始终绕过缓存。

开启补充会把待查询的公网 IP 发给第三方。纯本地设为 `false`（未配置也不联网补充）；`GEO_MODE=online` 本身始终在线，不受这个开关控制。后台“属地核验”保存最近一次查询的候选、来源版本、状态和时间，失败刷新也可查看，原属地快照保持独立；公开列表和本人订单接口不暴露核验细节。

## 常见问题

| 现象 | 检查 |
| --- | --- |
| 启动报库文件错误 | 检查路径、读权限、解压后的文件及 XDB IPv4 / IPv6 是否填反；在线补充不能兜底文件启动错误 |
| 启动正常却查不到省市 | City 库的个别网段也可能仅有国家；开启 `GEO_ONLINE_FALLBACK=true` 可在缺少省份时继续查 API。Country / ASN 库或不兼容字段不能替代 City 数据 |
| 本地可用但区县 / 运营商为空 | 数据包可能没有这些字段；不跨来源拼接细节，已有省份时不会仅为补齐市区继续请求 |
| 显示内网 / 反代的 IP 或属地为空 | 检查真实 IP 请求头和反代覆盖配置；内网、回环、保留地址不会查询 |
| 换库后旧订单没变化 | 结果按订单保存为快照；重启后用新建订单验证，或由管理员明确刷新选定旧订单，不自动覆盖旧数据 |
| 前台没有后台那么细 | 公开结果最多省级，后台展示实际可用细节；IP 属地不是用户的精确地址 |

更多部署设置见[运行说明](operations.md)，全部变量见 [`.env.example`](../.env.example)。
