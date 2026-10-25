# OronBox 管理后台运维手册

管理后台只有 `/console` 一个入口，前端源码在 OronBox-Server-Console 仓库。Server 从配置的 Gitea CI Release 安装构建包；登录及前端同步过渡页沿用 rewrite 的服务端模板，不依赖 MUI 构建包。管理 API 使用独立后台会话、同源检查和 CSRF。首次安装自动重试，已有构建通过设置页更新。

## 角色

- `reviewer`：查看后台数据；审核资源、插件、合集和评论；基于历史修订创建管理草稿。不能修改发布配置，也不能管理用户、硬币、设备、版本、设置或维护任务。
- `admin`：拥有 reviewer 能力，并可管理发布配置、用户、硬币、设备、运营内容、客户端版本、系统设置和维护任务。密钥只显示配置状态，不显示原文。

## 主要工作流

资源编辑不会覆盖历史修订。管理员从指定修订创建管理草稿，编辑元数据、媒体、安装包、设备、来源、合集、协作者及发布计划，提交后进入审核；批准后才切换当前修订。AstroBox 的付费类型映射固定为：`free` 发送空值、`paid` 发送 `paid`、`force_paid` 发送 `force_paid`。

插件包和合集同样使用不可变待审版本。合集启停、成员顺序和代表资源只在审核通过后原子生效。发布任务详情保留每次领取、执行、轮询、失败、重试和取消历史。

举报详情区分用户可见回复和后台内部备注，并保存目标快照与状态时间线。硬币账本不可删除；调账、冻结、解冻和作废都必须填写原因。

## 维护与安全

- OAuth 诊断会在 Store 层遮蔽 token、secret、code、ticket 和 Authorization，不读取密文凭据列。
- Blob 管理读取私有对象、重新入队副本等操作均审计。
- 数据清理必须先生成预览；预览令牌绑定管理员、截止时间且 10 分钟过期。执行时需输入页面给出的精确确认短语，删除在 Serializable 事务中原子完成。
- 审计日志支持目标、操作者、动作、结果和时间筛选、详情以及 CSV 导出。

## 上线前验证

```bash
gofmt -w .
go test ./...
go vet ./...
go build ./...
```

设置 `TEST_DATABASE_URL` 后再次执行 `go test ./...`，会在隔离的临时数据库中验证迁移、管理修订、合集、发布历史、工单、OAuth、审计和维护事务。不要把该变量指向生产数据库。

实际环境至少验证：reviewer/admin 权限边界；管理修订审核前后数据；三种 AstroBox 付费映射；失败发布重试；合集停用；插件包更新；会话撤销；硬币回滚；OAuth 脱敏；清理预览与过期令牌；移动端、键盘和深浅色显示。

## 后续事项

活跃统计按登录用户和北京时间去重，记录日活及版本/平台覆盖。用户协议、隐私说明留待后续核对和更新。

## Console OAuth 与会话

`GET /` 302 到 `https://oronbox.zxor.org`。后台授权入口为 `GET /console/api/auth/bandbbs/authorize`，仅申请 `user:read`，固定回调仍为 `/oauth2/bandbbs/callback`。`admin_` state 存储于独立 provider，标记 `console_login` 并绑定发起浏览器；回调校验后直接创建后台 Cookie，不创建客户端 ticket、AT/RT，也不保存或覆盖客户端米坛 grant。第三方令牌核验后撤销。客户端 state 使用 `client_login` / `client_publish`；ticket 仅允许客户端用途，并核验 app ID 和平台。部署前创建的未完成 Console 授权 state/ticket 会在迁移时清除。

后台端点统一为 `GET /console/api/auth/session`、`POST /console/api/auth/refresh`、`POST /console/api/auth/logout`。Cookie 为 HttpOnly、Secure（HTTPS）、SameSite=Lax、Path=/console，数据库只保存凭据哈希。每次授权通过的后台请求延长 12 小时闲置期限，最长不超过登录后 7 天。凭据距上次签发 11 小时后可按需轮换，MUI 在启动、切回前台和正常请求时检查，不后台轮询。并发 refresh 合并，旧凭据仅保留 30 秒重叠窗口并返回同一当前凭据。CSRF 绑定稳定会话 ID，轮换不使其他标签页的 CSRF 失效。

每次鉴权检查当前封禁和角色；401 表示会话无效，403 表示权限或 CSRF/同源拒绝，5xx 不清登录。旧明文后台会话凭据迁移为哈希，保留原始创建时间和有效期；客户端授权记录保持不变。旧 `/console/api/session`、`/console/logout` 和 `/console?ticket=...` 登录机制不再使用。

Console 构建契约为 `oronbox-console-v2`，同步包含独立 auth/session、auth/refresh、auth/logout 调用。旧 v1 包会被拒绝，后端和前端构建须成套上线；不可将旧包改写标识后继续使用。
