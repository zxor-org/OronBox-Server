# OronBox Server

OronBox 的开源资源服务端，使用 Go 构建

The open-source resource server for OronBox, built with Go

> ⚠️ OronBox Server 仍在积极开发，尚未达到生产可用状态
>
> ⚠️ OronBox Server is under active development and is not production-ready

## 功能状态 / Feature status

| 功能 / Feature | 状态 / Status |
| --- | --- |
| 米坛 OAuth 登录 / BandBBS OAuth sign-in | ✅ 已完成 / Complete |
| OronBox 资源浏览与下载 / OronBox resource discovery and downloads | ✅ 已完成 / Complete |
| VelaOS 快应用与表盘发布 / VelaOS quick app and watchface publishing | ✅ 已完成 / Complete |
| 资源修订与审核 / Resource revisions and review | ✅ 已完成 / Complete |
| 米坛同步发布 / BandBBS publishing | ✅ 已完成 / Complete |
| AstroBox-Repo 同步发布 / AstroBox-Repo publishing | ✅ 已完成 / Complete |
| 用户反馈与版本信息 / Feedback and release information | ✅ 已完成 / Complete |

## 技术栈 / Technology

| 组件 / Component | 用途 / Purpose |
| --- | --- |
| Go | HTTP 服务与业务逻辑 / HTTP service and application logic |
| PostgreSQL | 账号、资源、修订、审核与发布状态 / Accounts, resources, revisions, reviews, and publication state |
| 本地内容寻址存储 / Local content-addressed storage | 保存资源文件的权威副本 / Authoritative resource storage |
| Cloudflare R2 | 可选的资源副本与下载线路 / Optional resource replica and download route |
| BandBBS OAuth 与 API | 用户身份与米坛发布 / User identity and BandBBS publishing |
| GitHub API | AstroBox-Repo 发布 / AstroBox-Repo publishing |

## 运行 / Run

准备一个空的 PostgreSQL 数据库，复制 `.env.example` 并填写所需配置，然后运行：

Prepare an empty PostgreSQL database, copy `.env.example`, fill in the required configuration, and run:

```bash
go run ./cmd/server
```

检查代码：

Run checks:

```bash
gofmt -w .
go test ./...
```

## 许可证 / License

OronBox Server 采用 [GNU Affero General Public License v3.0](LICENSE) 许可证

OronBox Server is licensed under the [GNU Affero General Public License v3.0](LICENSE)
