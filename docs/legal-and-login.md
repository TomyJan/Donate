# 条款、隐私与登录配置

## 页面和确认

- 用户协议：`/terms`；隐私政策：`/privacy`。独立页面复用站点头尾，支持中英文、深浅色、手机浏览和直接访问。
- 登录弹窗、页脚、底部 Cookie 提示均有两个条文入口。登录弹窗中的链接在新标签页打开，保留当前输入与勾选。
- 首次登录不预选同意。勾选后才允许发起第三方登录；取消勾选立即禁止。Cookie 提示的“知道了”只收起提示，与条款勾选完全独立。
- `donate:legal-consent` 和 `donate:cookie-notice` 分别保存当前条款版本，原有 `donate-language`、`donate-theme` 不变。版本在 `web/src/lib/legal.ts` 统一维护；实质修改任一条款后必须升版本，两份双语条文同步更新。跨标签页的存储变更会同步；本地存储不可用时，只在当前页面临时记忆，不阻塞使用。
- Cookie 提示为页面底部非模态浮层，有进出动画及减少动态效果适配。页脚 Cookie 可重新打开。提示没有广告追踪开关，因为本站没有接入广告或统计追踪。
- 此次按用户要求仅保存本机确认状态，**不是服务器端的签约凭证或不可篡改同意记录**，也不为已有会话追溯补记同意。OAuth 回调及所有业务 API 仍独立执行鉴权。

协议说明自愿个人赠与、无商品或服务对价、无咨询/支持/回复等承诺，以及完成赠与原则上不退款；保留无效或可撤销行为、未成年人授权、支付差错及法律规定的其他返还情形。隐私政策按当前登录、订单、IP 属地、日志、第三方资源与 Cookie 实现撰写。联系渠道沿用个人主页已经公开的 Telegram 和 QQ 联系群，不虚构邮箱；请求者应私下联系管理员，不公开敏感凭证。

制定时核对的官方法律原文：

- [《民法典》](https://www.samr.gov.cn/zw/zfxxgk/fdzdgknr/bgt/art/2023/art_60fb3863b7364b759123a42d1df568ca.html)：格式条款提示与说明规则、赠与合同及法定撤销规则；不得用绝对“不退款”排除法定救济。
- [《个人信息保护法》](https://www.samr.gov.cn/wljys/gzzd/art/2023/art_3ef1e889c1e644d4b65b5f5c7f432386.html)：告知、必要最短保存期限及个人权利。

上线前应结合实际部署、支付服务商、联系渠道和处理范围复核条文；新增统计脚本、数据接收方或改变公开范围时，更新隐私披露与必要的同意流程。

## OIDC 显示与回调标识

`OIDC_ISSUER` / `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` 继续用于单一 OIDC 来源，新增：

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `OIDC_ID` | `oidc` | 外部登录/绑定标识，以及回调路径最后一段 |
| `OIDC_NAME` | `OIDC` | 登录按钮与身份绑定名称，1–64 字单行文本 |
| `OIDC_ICON_URL` | 空 | HTTPS 图标地址；留空或加载失败时使用通用图标 |

ID 使用 1–32 位小写字母、数字、下划线或连字符，以字母开头，不得为保留的 `qq`。例如 `OIDC_ID=campus` 应在身份提供者注册完整回调 `https://donate.example.com/api/v1/user/oauth/callback/campus`。名称与图标只改变展示。公开 `site/info.providers` 使用这些外部 ID，`provider_details` 提供 `id/kind/name/icon_url`；前端发起登录/绑定使用 `id`，关联既有绑定使用稳定的 `kind`。

数据库仍以 `provider=oidc`、原始 issuer、验证后的 subject 定位身份，因此改名称、图标或路由 ID 不会新建账号；修改 issuer 或上游 subject 则是不同身份，不能据昵称/头像自动合并。改 ID 必须同步上游回调白名单并重启，旧 ID 不作为别名接受，进行中的旧授权需要重来。

## QQ 的 UnionID 模式

`QQ_USE_UNIONID=false` 为默认值，保持 OpenID 登录。设为 `true` 前，先在 QQ 互联为当前应用取得 UnionID 权限；调用 `oauth2.0/me` 时传 `unionid=1`，校验返回的 client_id、OpenID 和 UnionID。缺 UnionID、接口错误或应用不匹配时拒绝登录，不静默降级到 OpenID。获取用户资料仍传 OpenID。

新增数据库迁移为既有 QQ 身份补充 `qq_openid`（来自原 subject），新增 `qq_unionid`，两列均不通过公开或账号资料 API 输出。开启后以 UnionID 为 subject，并保留 OpenID。已有 OpenID 匹配的身份原地升级，保留用户 ID、绑定 ID、原绑定时间、QQ 号、历史订单和原会话；不重新要求已核验用户填写 QQ 号。新用户仍走原有 QQ 号与头像核验，两种标识随加密授权流程一起保存。

两种标识指向不同身份、既有 OpenID 返回不同的非空 UnionID、尝试跨账号绑定时均拒绝，不合并用户或挪动绑定。关闭开关可按已保存的 OpenID 登录，已保存的 UnionID 不被清空。身份命名空间仍按当前 QQ_APP_ID 隔离；更换应用 ID 不视为同一登录来源，不做跨应用自动迁移。

迁移前保留原有自动备份。备份恢复校验从程序内嵌迁移文件识别支持版本，允许旧版本备份在启动时升级，拒绝未知未来版本；已有 UnionID 数据时禁止直接向下迁移，避免丢失绑定键。不要删除生产库来应用本次迁移。

接口依据：[QQ 互联 UnionID 说明](https://wiki.connect.qq.com/unionid%E4%BB%8B%E7%BB%8D/)、[获取 OpenID](https://wiki.connect.qq.com/%E8%8E%B7%E5%8F%96%E7%94%A8%E6%88%B7openid_oauth2-0/)。实际权限、QQ 头像一致性和生产 OIDC 仍须使用部署方真实应用联调；测试上游仅在 `e2e` 构建中使用。
