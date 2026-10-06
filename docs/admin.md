# OronBox 管理后台运维手册

## 入口与鉴权

- 控制台位于 `/admin/`，为内嵌静态单页应用（构建产物经 `//go:embed all:dist` 注入二进制）。
- 登录基于米坛（BandBBS）OAuth2 与环境变量 `ADMIN_BANDBBS_UIDS` 白名单，系统不设明文密码。
- 登录回调与客户端登录共用 `GET /oauth2/bandbbs/callback`，通过 `state` 的 `admin:` 前缀区分用途。
- 登录成功后服务端下发 `oronbox_admin` HttpOnly Cookie；会话记录于 `admin_sessions`，有效期 24 小时。
- 所有 `/admin/api/*` 请求经校验 Cookie 会话。

## 接口说明

- 管理端接口支持资源审核、用户管理与定向私信、工单客服、金币审计调账、系统设置、版本同步与鉴权吊销等操作。
- 管理端路由前缀统一为 `/admin/api/`。

## 安全与审计

- 敏感写操作统一记录于 `audit_logs`（包含操作人、动作、结果、IP 与时间戳）。
- 支持客户端构建鉴权两级吊销策略（全局最低版本约束与单版本黑名单）。

## 验证与构建

```bash
gofmt -w .
go vet ./...
go test ./...
CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/oronbox-server ./cmd/server
```
