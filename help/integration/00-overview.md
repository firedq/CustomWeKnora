# WeKnora Wrapper 集成方案 · 总览

> 本目录文档面向"在 WeKnora 之上**外挂一层包装服务**、对接调用方自有业务系统"的集成场景。**不修改 WeKnora 源码**，将 WeKnora 视为黑盒上游服务。
>
> 文档定位为 **设计 / 集成方式确认**，**尚未进入开发**。开发动作需在 [05-development-plan.md](./05-development-plan.md) 任务通过用户复核后启动。

---

## 1. 目标与约束

### 1.1 目标
为调用方自有服务对外暴露一套**完整生命周期**的 RESTful API：
1. 租户管理（创建 / 查询 / 删除 / 轮换 APIKey）
2. 用户管理（包装层用户，归属包装层租户）
3. 知识库管理（创建 / 查询 / 更新 / 删除 / 文件上传 / URL 导入 / 手工 Markdown）
4. 知识检索（混合检索、跨库语义检索，不走 LLM）
5. 对话能力（RAG 模式、Agent 模式，SSE 流式）
6. 文件下载 / 预览代理

### 1.2 硬约束
| # | 约束 | 影响 |
|---|---|---|
| C1 | **零侵入** WeKnora 源码（`internal/`、`cmd/`、`router/`、`migrations/` 等均不改动） | 排除 Go 插件 + build tag 等需重编 WeKnora 的方案 |
| C2 | WeKnora 后续版本升级**不产生合并冲突** | 排除任何 fork 内嵌策略 |
| C3 | WeKnora 凭证不向调用方泄露 | API Key / Bearer 全部由包装层托管 |
| C4 | 必须支持 SSE 流式对话与 multipart 文件上传 | 反代必须流式、零缓冲 |

---

## 2. 最终选型

> **方案 A：独立 BFF/Gateway 服务**（详见 [01-architecture.md](./01-architecture.md) 四方案对比）

- **语言**：Go（与 WeKnora 同栈，SSE / `httputil.ReverseProxy` / `mime/multipart` 一等公民）
- **形态**：独立进程（独立二进制 + 独立 DB），通过 HTTP 调用 WeKnora `/api/v1`
- **凭证**：包装层数据库存储 WeKnora 每租户 API Key（密文），运行时注入 `X-API-Key` 请求头
- **WeKnora 暴露**：私网，**不对外**，仅包装层可达

---

## 3. 文档导航

| 文档 | 内容 |
|---|---|
| [00-overview.md](./00-overview.md) | 本文：目标 / 约束 / 选型 / 决策点 |
| [01-architecture.md](./01-architecture.md) | 四方案对比与推荐方案详细论证 |
| [02-api-mapping.md](./02-api-mapping.md) | 包装层 ↔ WeKnora 端点逐项映射表 |
| [03-data-model.md](./03-data-model.md) | 包装层数据库 schema 设计 |
| [04-deployment.md](./04-deployment.md) | 部署拓扑、网络分层、配置项 |
| [05-development-plan.md](./05-development-plan.md) | 分阶段实现计划（用户确认后启动） |
| [06-coverage-and-upstream-tracking.md](./06-coverage-and-upstream-tracking.md) | 端点覆盖矩阵（A/B/C/D 四档）与 WeKnora 上游版本跟进 SOP |

---

## 4. 关键设计决策（已默认采纳推荐选项，可在开发前覆盖）

| # | 决策点 | 推荐默认 | 备选 |
|---|---|---|---|
| D1 | 包装层语言 | **Go** | Python / Node.js |
| D2 | 包装层位置 | **独立仓库** `weknora-wrapper/` | WeKnora 仓库根新增 `wrapper/` 顶层目录（不动 `internal/`） |
| D3 | "创建用户" 语义 | **包装层独立用户**，多用户共享同一 WeKnora 租户 | 一外部用户对应一 WeKnora 租户（强隔离、成本高） |
| D4 | 文件 URL 策略 | **包装层重新签名**，不外泄 WeKnora 路径 | 透传 WeKnora 预签名链接（省事，但泄露内部信息） |
| D5 | WeKnora 注册接口可用性 | 部署时确认 `/api/v1/auth/register` **允许**包装层调用（生产中如禁用，则需备用开通通道） | 改用跨租户管理员账户 + `POST /tenants` |
| D6 | 文件代理路径 | 包装层 `/v1/files/:id`（重新签名） | 直接透传 `/files?file_path=...` |

---

## 5. WeKnora 现状速查（Phase 1 验证结论）

| 项 | 结论 | 源 |
|---|---|---|
| Gin 路由聚合入口 | `internal/router/router.go` | router.go:75 (`NewRouter`) |
| 全局认证中间件 | `internal/middleware/auth.go` | auth.go:67 (`Auth`) |
| 认证模式 | `Authorization: Bearer <jwt>` **或** `X-API-Key` 头，互斥 | auth.go:86-223 |
| 免认证白名单 | `/health`, `/auth/login`, `/auth/register`, `/auth/refresh`, `/auth/oidc/*`, `/files/presigned` | auth.go:21 (`noAuthAPI`) |
| 租户创建端点（需鉴权） | `POST /api/v1/tenants` | tenant.go:81 (`CreateTenant`) |
| 用户+租户原子注册 | `POST /api/v1/auth/register` | auth.go (Register), help/multi-tenancy.md:21 |
| APIKey 明文返回时机 | **仅** `CreateTenant` 与 `ResetAPIKey` 响应；DB 存密文 | tenant.go (`CreateTenant`, `ResetAPIKey`) |
| 跨租户访问 | 双重门控：`config.Tenant.EnableCrossTenantAccess` AND `user.CanAccessAllTenants` | auth.go:49 (`canAccessTenant`), help/multi-tenancy.md |
| SSE 对话端点 | `POST /api/v1/knowledge-chat/:session_id`、`POST /api/v1/agent-chat/:session_id` | router.go:359-376 |
| Multipart 上传 | `POST /api/v1/knowledge-bases/:id/knowledge/file` | router.go:201 |
| 文件代理 | `GET /files?file_path=...`（鉴权）、`GET /api/v1/files/presigned`（HMAC，免鉴权） | router.go:777-963 |

---

## 6. 下一步

待本目录全部文档审阅通过后，进入 [05-development-plan.md](./05-development-plan.md) 启动开发；开发期间任何对 WeKnora **源码** 的修改需求都应**立即叫停**并回到本目录修订方案。
