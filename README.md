# Donate

[donate.tomys.top](https://donate.tomys.top) 新版打赏站。界面与交互设计已定稿，当前仓库保存最终设计源码、可运行的前后台参考页面和实现规格；Go 业务服务尚未实现。

新版采用单实例 Go + SQLite，前端构建产物由 `go:embed` 内嵌，按版本统一打包发布。支持 QQ / 自定义 OIDC 登录、账号绑定、易支付支付宝 / 微信、捐赠历史和管理后台。首页采用简洁的收据册布局，适配手机、深浅色及中英文。

- [产品与实现约束](docs/requirements.md)
- [最终界面、组件和交互规范](docs/design/README.md)
- [IP 属地数据方案](docs/ip-geolocation.md)
- [首页 HTML](docs/design/preview/index.html) / [后台 HTML](docs/design/preview/admin.html)

运行最终设计参考页面（Python 3.10+，无需 npm 或 Go）：

```sh
python docs/design/serve.py
```

打开 <http://127.0.0.1:52771/> 与 <http://127.0.0.1:52771/admin>。也可直接打开两份 HTML；推荐本地服务以获得跨页演示状态、正确路由和当前 Git 版本链接。默认使用管理员示例身份，登录、支付和管理操作均为本地演示。

编辑 `docs/design/src/` 后生成并检查：

```sh
python docs/design/build.py
python docs/design/build.py --check
```

`master` 仅保存新版最终设计；旧站 PHP、运行说明与易支付逻辑参考在独立的 `legacy` 分支。正式前端框架及 Go 库将在实现阶段确定，不将本设计的原生 JavaScript 控制器当作生产业务架构。

项目许可证见 [LICENSE](LICENSE)，图标授权见 [第三方资源说明](docs/design/vendor/README.md)。
