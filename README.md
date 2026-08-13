<p align="center">
  <img src="https://raw.githubusercontent.com/zxor-org/OronBox/main/assets/images/app_icon.png" width="112" alt="OronBox">
</p>

<h1 align="center">OronBox Server</h1>

<p align="center">OronBox 的资源、审核与跨平台发布服务</p>

<p align="center">
  <a href="https://github.com/zxor-org/OronBox-Server/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/zxor-org/OronBox-Server/ci.yml?label=CI" alt="CI status"></a>
  <a href="https://github.com/zxor-org/OronBox-Server/releases"><img src="https://img.shields.io/github/v/release/zxor-org/OronBox-Server?display_name=tag&sort=semver&label=release" alt="Latest release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/zxor-org/OronBox-Server" alt="License"></a>
  <a href="https://github.com/zxor-org/OronBox-Server/stargazers"><img src="https://img.shields.io/github/stars/zxor-org/OronBox-Server?style=flat" alt="GitHub stars"></a>
</p>

OronBox Server is the production resource service behind OronBox. It provides the shared resource catalog, creator revisions, moderation, downloads, user feedback and publication integrations for BandBBS and AstroBox-Repo.

## 快速入口 / Quick links

- [配置示例 / Configuration](.env.example)
- [后台运维手册 / Operations guide](docs/admin.md)
- [问题反馈 / Issues](https://github.com/zxor-org/OronBox-Server/issues)
- [客户端 / OronBox](https://github.com/zxor-org/OronBox)

## 技术栈 / Technology

| 组件 / Component | 用途 / Purpose |
| --- | --- |
| Go | HTTP 服务与业务逻辑 / HTTP service and application logic |
| PostgreSQL | 账号、资源、修订、审核与发布状态 / Accounts, resources, revisions, reviews, and publication state |
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

管理后台的角色边界、资源审核、发布控制、维护操作与上线验证流程见 [后台运维手册](docs/admin.md)

## 许可证 / License

OronBox Server 采用 [GNU Affero General Public License v3.0](LICENSE) 许可证

OronBox Server is licensed under the [GNU Affero General Public License v3.0](LICENSE)
