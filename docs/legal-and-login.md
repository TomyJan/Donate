# 登录与条款配置

## OIDC

配套填写 `OIDC_ISSUER` 与 `OIDC_CLIENT_ID`，`OIDC_CLIENT_SECRET` 按提供方要求填写，可为空。提供方需支持授权码流程、PKCE S256 及 `openid profile`。

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `OIDC_ID` | `oidc` | 登录标识和回调路径 |
| `OIDC_NAME` | `OIDC` | 按钮与绑定名称 |
| `OIDC_ICON_URL` | 空 | HTTPS 图标；留空使用默认图标 |

回调：`https://站点域名/api/v1/user/oauth/callback/<OIDC_ID>`。ID 使用 1–32 位小写字母、数字、下划线或连字符，以字母开头，不得为 `qq`。

修改 ID 后同步提供方的回调地址；名称、图标和 ID 的变化不影响既有账号。更换 issuer 则属于不同身份。

IdP 应用入口可设为 `https://站点域名/api/v1/user/login?provider=<OIDC_ID>&return_to=/`：浏览器 GET 导航直接 302 到授权页，不应以 fetch 调用。`return_to` 可省略，仅保留 `/`、`/admin` 或 `/?payment=returned&order=<订单UUID>`，其他值回到 `/`。QQ 登录将 `provider` 改为 `qq`；绑定账号仍使用独立的 POST + CSRF 接口。

## QQ

配置 `QQ_APP_ID`、`QQ_APP_SECRET`，回调为 `https://站点域名/api/v1/user/oauth/callback/qq`。首次登录或绑定须完成 QQ 号验证。

默认使用 OpenID。启用 `QQ_USE_UNIONID=true` 前，先取得 QQ 互联的 UnionID 权限；启用后以 UnionID 识别身份，同时保留 OpenID，缺少 UnionID 会拒绝登录。已有绑定原地升级，关闭后仍可通过 OpenID 登录；更换应用 ID 不会自动迁移账号。

## 账号

会话有效期为 30 天，仅在剩余有效期不超过 20 天时，活跃操作才续至当前时间起 30 天。前端仅在页面可见且近期有活动时按窗口触发，不在窗口外轮询，也不靠后台计时续活长期闲置或已失效的会话。

普通用户不能解绑；管理员可在用户详情解绑，但必须保留至少一种登录方式。

封禁会撤销已有会话，并阻止所有绑定身份登录。授权完成页显示“登录失败：您的账号已被封禁，如有疑问请联系站长。”，解禁后需重新登录。

OIDC 头像使用提供方返回的地址。HTTP 地址先升级 HTTPS 并验证可读取的图片；失败不保存不安全地址，并保留已有头像。没有头像时以默认头像展示。诊断需保留重新登录的日志，不能仅凭页面请求日志判断上游字段。

## 条款与隐私

用户协议位于 `/terms`，隐私政策位于 `/privacy`。站内登录弹窗要求主动勾选，本机保存选择；外部 IdP 直接登录入口不会经过该弹窗，这不是服务端存储的协议签署记录。Cookie 弹窗确认独立保存。

上线前核对联系渠道、支付服务商和隐私披露。条款文案在 `web/src/lib/i18n.ts`，修改须同步中英文，并更新 `web/src/lib/legal.ts` 中的版本。
