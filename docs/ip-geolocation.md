# IP 属地配置

## 选择模式

| 配置 | 行为 |
| --- | --- |
| `GEO_MODE=online` | 默认值，使用内置在线查询与故障转移，无需下载数据或填写 API Key |
| `GEO_MODE=local` | 读取 `GEO_DATABASE` 指定的本地 MMDB；仅在开启 `GEO_ONLINE_FALLBACK` 时允许在线补充 |
| `GEO_MODE=disabled` | 不查询属地，仍记录下单 IP |

## 本地库怎么配

1. 从下面的数据源下载 **City / 城市级 MMDB**，解压得到 `.mmdb` 文件。不要把压缩包、CSV、Country 或 ASN 库当成城市库。
2. 放在程序可读的位置，修改启动工作目录里的 `.env`，例如：

```dotenv
GEO_MODE=local
GEO_DATABASE=./GeoLite2-City.mmdb
GEO_ONLINE_FALLBACK=true
```

3. 重启程序。通过公网访问并新建订单，在后台订单详情查看下单 IP、属地及来源；来源为 `mmdb` 表示使用了本地查询结果。

文件放在当前目录即可，不要求固定目录；也可使用其他相对路径或绝对路径。相对路径以**启动工作目录**为准，不是二进制所在目录或 `DATA_DIR`；路径含空格时加引号。程序不附带本地库，也不负责下载或自动更新。

当前只读取一个文件，并按 **MaxMind City 字段结构**解析：`country.names`、`subdivisions[].names`、`city.names` 等。名称优先 `zh-CN`，缺失时取 `en`；省、市、第二级行政区、坐标、ISP / ASN 等仅在来源实际提供对应字段时保存，不混合不同来源的字段、不推测缺失信息。不是所有 `.mmdb` 都符合这一结构，更不能通过改扩展名接入其他格式。

## 数据源推荐

### 匹配当前解析器

| 数据源 | 选择建议 | 下载与注意事项 |
| --- | --- | --- |
| [MaxMind GeoLite2 City](https://dev.maxmind.com/geoip/geolite2-free-geolocation-data/) | 免费入门，先验证本站实际省市显示是否够用 | 注册账号，从账户下载城市级 MMDB；需遵守 GeoLite 署名及更新要求 |
| [MaxMind GeoIP City](https://dev.maxmind.com/geoip/docs/databases/city-and-country/) | 付费候选，与现有解析器同一字段体系 | 选择 `GeoIP2-City.mmdb`，订阅更新；不要误选独立 Country / ASN / ISP 产品 |
| [DB-IP IP to City Lite](https://db-ip.com/db/download/ip-to-city-lite) | 免费替代，适合先比较大陆 IPv4 / IPv6 样本 | 选择 MMDB，解压 `.mmdb.gz` 后配置实际文件名；按月更新，展示结果的页面须按其要求署名并链接 DB-IP |
| [DB-IP IP to Location](https://db-ip.com/db/ip-to-location) | 付费候选，可与 MaxMind 比较省市与第二级行政区覆盖 | 选择 MMDB，按订阅获取更新；不是在线 API 套餐 |

两家数据的字段结构分别见 [MaxMind City](https://dev.maxmind.com/geoip/docs/databases/city-and-country/)、[DB-IP Lite](https://db-ip.com/db/format/ip-to-city-lite/mmdb.html) 和 [DB-IP Location](https://db-ip.com/db/format/ip-to-location/mmdb.html)。这表示字段可接入，不代表已验证所有中国 IP 的精度；正式选型用已知位置的宽带、移动网络、校园网及 IPv6 样本比较。

免费库也有使用条件：[GeoLite 对外展示要求署名](https://support.maxmind.com/knowledge-base/articles/sell-or-display-data-from-geolite-databases-and-web-services)，[DB-IP Lite 下载页](https://db-ip.com/db/download/ip-to-city-lite)列明页面署名要求。本程序不会根据所选库自动添加来源署名，部署方需按所用数据的要求补充。

### 中国区县数据候选，当前需要另行适配

| 数据源 | 适用方向 | 当前限制 |
| --- | --- | --- |
| [IPIP.NET](https://www.ipip.net/product/ipv6.html) | 中国城市 / 区县、运营商等，按产品选购 IPv4 / IPv6 数据 | 官方 IPDB 不能直接填入 `GEO_DATABASE`，需增加解析适配；公开展示结果的授权需向供应商确认 |
| [纯真商业离线库](https://www.cz88.net/) | 中国省市区县数据候选，可向供应商索取样本比较 | 需适配交付格式及字段，不能仅填路径启用 |
| [ip2region](https://github.com/lionsoul2014/ip2region/blob/master/README_zh.md) | 免费双栈城市数据；对更新与精度要求更高时可比较其商用数据 | 当前不支持 XDB，需要新增读取适配；开源查询引擎不等于所有数据包均可任意使用 |

希望现在直接离线运行，先试上面的 City MMDB；若中国区县覆盖是首要要求，再向国内服务商要样本，确定数据与授权后做相应适配。不要因为“支持中文”就假定每个地址都有省、市、区县。

## 更新与验证

- 按供应商要求定期更新。MaxMind 可用官方 [geoipupdate](https://dev.maxmind.com/geoip/updating-databases/) 下载；下载器的账号 / License Key 不填写到 Donate 的配置中。
- 新库先下载并解压到另一个临时文件，确认不是错误页面或压缩包；停服后替换 `GEO_DATABASE` 指向的文件，再启动。保留上一份以便回退，不要原地截断或覆盖进程正在读取的文件。
- 更新工具只负责文件，Donate **不热加载**，更换库后仍需重启。新查询使用新库，已有订单的属地快照不会自动重算。管理员可在订单详情或批量操作中明确选择“刷新属地”（包括迁移订单），按当前模式绕过应用内 IP 缓存重新查询；取得国家及省份后替换快照，失败或缺省份时保留原值，按任务策略重试，不改变支付状态。禁用模式会提示不可刷新，重复操作共享正在等待或执行的刷新任务。
- 可安装 `mmdblookup` 验证文件及字段，例如：

```sh
mmdblookup --file ./GeoLite2-City.mmdb --ip 223.5.5.5
```

示例 IP 仅用于检查读取是否正常，不作为准确率样本。最终用自己的公网 IPv4 / IPv6 和已知实际位置核对；本机 `127.0.0.1` 或内网地址不查询属地。经反代部署时先核对 `REAL_IP_HEADER`，反代应覆盖该头并限制源站直连，避免把反代 IP 误当访客 IP。

## 在线查询与补充

内置顺序为：

- IPv4：**IP9 → IP优 → I5P → 太平洋**。
- IPv6：**IP9 → IP优 → I5P**，不向太平洋发送 IPv6。

查询有缓存、限流、超时和失败切换，不阻塞支付。某个来源只返回国家、没有省份时会继续查询下一个来源；取得省份后停止，不为补齐市、区县而继续联网。全部来源都无法提供省份时，新订单仍保存已取得的国家等部分信息，前台省份仍显示未知，部分结果只缓存 5 分钟。已有订单刷新不使用不完整结果覆盖原值。当前没有在线服务商的自定义 URL / Key 配置项。

本地模式建议设置 `GEO_ONLINE_FALLBACK=true`：本地查询报错、未命中或缺少省份时走同一在线链，采用首个包含国家及省份的完整来源结果。只有市、区县缺失不会触发补充。开启补充会把待查询的公网 IP 发给第三方；需要纯本地查询时明确设为 `false`（未配置时也不联网补充）。`GEO_MODE=online` 本身始终在线，不受这个开关控制。

## 常见问题

| 现象 | 检查 |
| --- | --- |
| 启动报库文件错误 | 检查 `GEO_DATABASE` 的工作目录、读权限及解压后的文件格式；文件打不开会启动失败，在线补充不能兜底启动 |
| 启动正常却查不到省市 | City 库的个别网段也可能仅有国家；开启 `GEO_ONLINE_FALLBACK=true` 可在缺少省份时继续查 API。Country / ASN 库或不兼容字段不能替代 City 数据 |
| 本地可用但区县 / 运营商为空 | 数据包可能没有这些字段；已有省份时不会仅为补齐其他细节触发在线补充 |
| 显示内网 / 反代的 IP 或属地为空 | 检查真实 IP 请求头和反代覆盖配置；内网、回环、保留地址不会查询 |
| 换库后旧订单没变化 | 结果按订单保存为快照；重启后用新建订单验证，或由管理员明确刷新选定旧订单，不自动覆盖旧数据 |
| 前台没有后台那么细 | 公开结果最多省级，后台展示实际可用细节；IP 属地不是用户的精确地址 |

更多部署设置见[运行说明](operations.md)，全部变量见 [`.env.example`](../.env.example)。
