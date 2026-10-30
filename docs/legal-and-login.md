# 登录与条款配置

## OIDC

配置 `OIDC_ISSUER`、`OIDC_CLIENT_ID` 和 `OIDC_CLIENT_SECRET`，提供方需支持授权码流程、PKCE S256 及 `openid profile`。

| 变量 | 默认值 | 用途 |
| --- | --- | --- |
| `OIDC_ID` | `oidc` | 登录标识和回调路径 |
| `OIDC_NAME` | `OIDC` | 按钮与绑定名称 |
| `OIDC_ICON_URL` | 空 | HTTPS 图标；留空使用默认图标 |

回调：`https://站点域名/api/v1/user/oauth/callback/<OIDC_ID>`。ID 使用 1–32 位小写字母、数字、下划线或连字符，以字母开头，不得为 `qq`。

修改 ID 后同步提供方的回调地址；名称、图标和 ID 的变化不影响既有账号。更换 issuer 则属于不同身份。

## QQ

配置 `QQ_APP_ID`、`QQ_APP_SECRET`，回调为 `https://站点域名/api/v1/user/oauth/callback/qq`。首次登录或绑定须完成 QQ 号验证。

默认使用 OpenID。启用 `QQ_USE_UNIONID=true` 前，先取得 QQ 互联的 UnionID 权限；启用后以 UnionID 识别身份，同时保留 OpenID，缺少 UnionID 会拒绝登录。已有绑定原地升级，关闭后仍可通过 OpenID 登录；更换应用 ID 不会自动迁移账号。

## 账号

会话空闲 30 天失效，活跃时持续续期。普通用户不能解绑；管理员可在用户详情解绑，但必须保留至少一种登录方式。

## 条款与隐私

用户协议位于 `/terms`，隐私政策位于 `/privacy`。登录前须主动勾选同意，本机保存选择；Cookie 提示的确认独立保存。

上线前核对条款中的联系渠道、支付服务商和隐私披露。修改条款须同步中英文，并更新 `web/src/lib/legal.ts` 中的版本。
