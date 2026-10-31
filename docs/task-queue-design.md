# 后台任务队列

沿用单实例 SQLite 队列，管理支付核验（payment）、属地查询（geo）和关单（close），不引入外部队列。现有租约、领取、退避、支付幂等与回调优先级不变。

## 管理界面

- 复用站点头部、后台导航、标题、日期选择、表格、分页、弹窗、确认浮层和消息。新增“任务”导航。
- 当前状态：等待执行 ready、执行中 running、已完成 done、失败 failed。待执行中区分到期可执行、未来计划；租约过期任务标注等待恢复，不冒充仍在正常执行。
- 状态统计反映当前队列；趋势时间范围只影响执行趋势，默认 7 天，支持已有时间选择器的所有范围。趋势零填充、限制点数，长范围自动分桶。
- 列表支持状态、类型、来源、关键词筛选，关键词匹配任务 ID、订单 ID、商户订单号与昵称。日期、类型、目标、状态、次数、计划时间、更新时间尽量支持表头排序。下方复用分页及每页数量选择。
- 详情展示任务和订单信息、来源、请求 ID、调度/租约时间、当前结果、总执行次数及按页加载的持久时间线。目标订单点击复用订单详情弹窗。日志包含任务与订单唯一标识，结果与来源使用中英可读映射；未知结果保留原值，错误信息需脱敏及限制长度。
- 支持失败任务重试、待执行任务立即执行；执行中和已完成任务不重放。单项和批量操作都需紧凑确认，批量控件仅选中后占用原有标题操作区。返回逐项结果，不掩盖不适用或冲突。
- 自动刷新仅任务页所需查询；页面隐藏、退出任务页、会话退出或失去权限时停止。刷新保留筛选、页码、滚动、选中和详情，不重挂载整页，不播重复入场动画。提供手动刷新与最后更新时间。
- 响应式、深浅色、键盘操作和 reduced motion 沿用现有规范。正文少、布局紧凑，移动端无整页横向溢出。

## 持久化与安全

- 迁移只增加历史/遥测结构，不修改既有任务和订单结果。覆盖所有入队、领取、重新领取、重试调度、完成与失败路径，包括付款回调直接完成关联任务、批量关单及属地刷新。状态改变与历史记录在同一事务中，不因请求失败留下假日志。
- 已有任务仅记升级时快照，明确历史日志开始时间；不臆造过去的次数、耗时或成功趋势。新增记录使用 UUID，不使用自增 ID。订单物理删除时按现有约定清除关联任务与历史。
- 管理员重试经过会话、权限和 CSRF 校验；事务内复核管理员未封禁。输入 ID、类型、状态、日期、排序字段和数量严格校验。每批最多 100 个 ID。
- 重试以原任务为单位保留历史，重置失败任务的本轮重试预算，不创建同一订单同类的重复活动任务，不夺取未过期租约。ready 任务仅提前调度；同一幂等键重复请求不能二次重置或重复执行。
- 记录管理员审计。审计目标使用关联订单，确保现有可读目标和订单详情复用仍正确；操作标签增加 task.retry。不展示租约令牌、密钥、OAuth 凭证或原始支付响应。
- 不增加暂停、删除任务、清空队列、强制完成支付等破坏性操作。

## API 契约

沿用 /api/v1、GET/POST、业务 200 包装和基础鉴权 40x。OpenAPI 由 Go DTO 生成，前端类型跟随生成。时间均为 RFC3339 UTC，趋势按 Asia/Shanghai 日期分桶。

- GET /admin/task/list：page（默认 1）、page_size（默认 20，最大 100）、status、kind、source、search、from/to（创建时间）、sort、direction。返回标准 Page<TaskItem>。
- GET /admin/task/detail?id=...：返回 TaskDetail。
- GET /admin/task/history?id=...&page=1&page_size=20：标准 Page<TaskEvent>，新到旧稳定排序。
- GET /admin/task/overview?range=7d：也支持 today/yesterday/30d/90d/all/custom；custom 带 from/to。返回 TaskOverview。
- POST /admin/task/retry：{ids: string[], idempotency_key: string}，返回 TaskRetryResult。

对接类型（字段名为固定接口，额外只可增可选字段）：

```ts
type TaskItem = {
  id: string; kind: string; order_id: string; merchant_order_no: string; nickname: string;
  status: string; attempts: number; total_attempts: number; source: string; result: string;
  created_at: string; updated_at: string; run_at: string; lease_until?: string;
  retryable: boolean;
};
type TaskDetail = TaskItem & {
  actor_id?: string; actor_name?: string; request_id: string; recording_since: string;
};
type TaskEvent = {
  id: string; event: string; status: string; attempt: number; source: string; result: string;
  message: string; created_at: string; run_at?: string; duration_ms?: number;
  actor_id?: string; actor_name?: string; request_id?: string;
};
// event: snapshot | created | started | recovered | rescheduled | retried | completed | failed
type TaskOverview = {
  total: number; counts: {ready: number; running: number; done: number; failed: number};
  due: number; scheduled: number; stale_running: number; recording_since: string;
  from: string; to: string;
  trend: {date: string; created: number; started: number; completed: number; failed: number; retried: number}[];
};
type TaskRetryResult = {
  queued: number; items: {id: string; result: string}[];
};
// result: queued | already_ready | running | done | active_conflict | not_found | not_applicable
```

## 验证与交付

先用失败测试证明历史遗漏、重复重试、非法输入或权限缺失，再实现。覆盖过期租约、回调竞态、重试幂等、状态筛选、稳定分页、空范围趋势、迁移快照和订单级联删除。浏览器覆盖真实管理员登录与队列读取、详情分页、单/批重试、自动刷新、错误恢复、320/390/768/桌面与深浅色。完成全套前后端检查、生成校验、生产构建、完整 E2E 与独立审查。

仅在 master 本地分笔提交；优先签名，签名不可用时允许单次 git -c commit.gpgsign=false，不改全局设置，不推送，不发版，不修改真实数据库或 .env。
