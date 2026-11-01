# 文档索引

## 部署与使用

| 文档 | 内容 |
| --- | --- |
| [项目首页](../README.md) | 最短部署和开发入口 |
| [运行说明](operations.md) | 配置、反代、日志、任务、备份恢复及升级 |
| [登录与条款](legal-and-login.md) | QQ / OIDC 回调、会话、条款维护 |
| [支付接入](payment-integration.md) | 易支付配置、协议边界和上线核对 |
| [IP 属地](ip-geolocation.md) | XDB / MMDB 来源、自动下载、回退和排障 |
| [配置模板](../.env.example) | 全部环境变量及默认值 |

## 开发与维护

| 文档 | 内容 |
| --- | --- |
| [开发指南](development.md) | 目录职责、开发环境、生成链与检查命令 |
| [产品规则](requirements.md) | 当前功能及权限、支付、隐私约束 |
| [架构](backend-design.md) | 服务边界、数据一致性、会话和任务 |
| [任务队列](task-queue-design.md) | 状态、执行历史及恢复语义 |
| [API](../api/openapi.json) | 从 Go 类型生成的接口契约 |
| [界面参考](design/README.md) | 冻结视觉基准及当前组件入口 |
| [后续方向](roadmap.md) | 尚未排期的开发建议与取舍 |

[历史计划](archive/README.md)仅用于追溯，不作为当前行为或开发指令。部署配置以配置模板为准；接口字段以生成契约为准，功能行为以代码与测试为准。文档与实现不一致时应同步修正。
