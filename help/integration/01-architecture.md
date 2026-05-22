# 集成架构选型 · 四方案对比与推荐论证

> 本文回答"如何把 WeKnora 包装为一层对外 API"的**架构选型**问题。结论：**方案 A · 独立 BFF/Gateway**。

---

## 1. 评估维度

包装层不是简单的反代，而是要承担**业务身份与凭证编排**。任何方案必须同时满足：

| 维度 | 说明 |
|---|---|
| M1 路由 / 鉴权转换 | 调用方业务身份 → WeKnora 租户 + APIKey |
| M2 租户开通 | WeKnora 的"一用户一租户"模型如何对接外部租户概念 |
| M3 凭证生命周期 | API Key 加密落库、轮换、撤销、最小化暴露 |
| M4 SSE 流式对话 | `text/event-stream` 零缓冲透传，支持取消 |
| M5 Multipart 上传 | 流式转发，避免内存爆炸 |
| M6 文件代理 | `/files`、`/files/presigned` 的封装策略 |
| M7 升级安全 | WeKnora 版本迭代时的合并冲突风险 |
| M8 业务可观测 | 审计 / 配额 / 限流 / 幂等 |

---

## 2. 候选方案

### 方案 A · 独立 BFF/Gateway 服务

#### 机制
另起一个 Go 服务作为对外 API 入口。它：
1. 用包装层自有方式验证客户端（JWT/Session/API Key/OIDC）
2. 解析包装层身份 → 映射到 WeKnora 租户 → 取出加密的 WeKnora API Key
3. 注入 `X-API-Key` 头反代到 WeKnora `/api/v1`
4. SSE 端点用 `httputil.ReverseProxy`（自定义 `FlushInterval = -1` 即每写即刷）
5. Multipart 上传用流式 body 转发，不缓存
6. 文件下载可直连透传，或先签名再代理（推荐后者）

#### 优劣
**优势**
- WeKnora **完全黑盒**，零侵入
- 业务 API 契约稳定，与 WeKnora 演进解耦
- 包装层是放置**审计 / 配额 / 限流 / 幂等 / 凭证轮换**的唯一合适位置
- 升级 / 切换 / 分片 WeKnora 时只需改适配器

**劣势**
- 需新建并维护独立服务、独立 DB
- SSE 和 multipart 的反代必须仔细测试

#### M1-M8 评分
| M1 | M2 | M3 | M4 | M5 | M6 | M7 | M8 |
|---|---|---|---|---|---|---|---|
| ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

---

### 方案 B · Sidecar 反代（nginx / Envoy + Lua/Wasm 滤镜）

#### 机制
nginx / OpenResty / Envoy 在 WeKnora 前面做反代，通过配置注入 `X-API-Key`、做路径前缀映射。复杂身份映射要写 Lua/Wasm 并外挂 Redis/Postgres。

#### 优劣
**优势**
- 部署轻量，运维熟悉
- SSE 配置好 `proxy_buffering off`、`X-Accel-Buffering: no` 后稳定
- 零 WeKnora 改动

**劣势**
- 多步租户开通（注册 → 登录 → 取 APIKey → 加密落库）写在 Lua 里**不可维护**
- 凭证轮换、事务、幂等几乎无法做
- 走到一定复杂度后会被迫前置一个"开通服务"，等于走回方案 A 的弱化版

#### M1-M8 评分
| M1 | M2 | M3 | M4 | M5 | M6 | M7 | M8 |
|---|---|---|---|---|---|---|---|
| ✅ | ❌ | ❌ | ✅ | ⚠️ | ⚠️ | 🟡 | ❌ |

---

### 方案 C · Go 插件 / 平行目录 + build tag

#### 机制
在 WeKnora 仓库新增 `plugins/wrapper/` 等平行目录，靠 build tag 控制是否编译进二进制，注册额外路由、复用 `interfaces.TenantService` 等内部接口。

#### 优劣
**优势**
- 同进程调用，无 HTTP 反代开销
- 可直接复用 WeKnora 类型与服务

**劣势**
- Go build tag **不是插件机制**：要么由 WeKnora `cmd/server/main.go` 显式 import，要么换 `main`。任意做法都**等价于改 WeKnora 源码或 fork**
- 一旦 `internal/` 包（特别是 `container`、`router`、`service`、`types/interfaces`）变更签名，包装层立刻断裂
- WeKnora 的 `internal/` 路径还禁止外部模块直接 import

#### M1-M8 评分
| M1 | M2 | M3 | M4 | M5 | M6 | M7 | M8 |
|---|---|---|---|---|---|---|---|
| ✅ | ✅ | ⚠️ | ✅ | ✅ | ✅ | ❌ | 🟡 |

> 决定性否决项：**M7（升级安全）失败** —— 与 C2 约束（升级零冲突）冲突。

---

### 方案 D · docker-compose 直通

#### 机制
WeKnora 原样跑起来，调用方业务系统直接持有 WeKnora API Key 并直连 `/api/v1`。

#### 优劣
**优势**
- 零开发量

**劣势**
- WeKnora API 形状、凭证全部泄露到调用方
- 租户开通、用户体系、审计、配额都落到调用方
- 等同于"不做包装"

#### M1-M8 评分
| M1 | M2 | M3 | M4 | M5 | M6 | M7 | M8 |
|---|---|---|---|---|---|---|---|
| ❌ | ❌ | ❌ | 🟡 | 🟡 | ❌ | ✅ | ❌ |

---

## 3. 选型矩阵

| 维度 | A 独立 BFF | B Sidecar | C Go 插件 | D 直通 |
|---|---|---|---|---|
| 不改 WeKnora 源（C1） | ✅ | ✅ | ⚠️ | ✅ |
| 升级零冲突（C2） | ✅ | ✅ | ❌ | ✅ |
| 凭证不外泄（C3） | ✅ | 🟡 | ✅ | ❌ |
| SSE / Multipart（C4） | ✅ | 🟡 | ✅ | 🟡 |
| 租户开通能力 | ✅ | ❌ | ✅ | ❌ |
| 凭证轮换 / 审计 | ✅ | ❌ | 🟡 | ❌ |
| 升级安全 | ✅ | 🟡 | ❌ | ✅ |

**结论：方案 A 是唯一同时满足全部硬约束、且能承载业务侧需求的方案。**

---

## 4. 方案 A 详细架构

### 4.1 分层

```
┌─────────────────────────────────────────────────────────────┐
│ 调用方业务系统 / SaaS / 内部产品                            │
└──────────────────────────┬──────────────────────────────────┘
                           │  包装层自有协议（JWT/Session/...）
                           ▼
┌─────────────────────────────────────────────────────────────┐
│  Wrapper BFF (Go)                                           │
│  ─────────────────────────────────────────────────────────  │
│  HTTP 接入层    │ Auth(JWT/APIKey) | Request-ID | Logger    │
│  Handler 层    │ Tenant / User / KB / Knowledge / Chat /   │
│                │ Search / Files                            │
│  Service 层    │ Provisioner / KeyRotator / Auditor /      │
│                │ RateLimiter / IdempotencyStore            │
│  WeKnora       │ http.Client + httputil.ReverseProxy       │
│  Adapter 层    │ 注入 X-API-Key, 流式 SSE / Multipart      │
│  Store 层      │ wrapper_tenants / wrapper_users /          │
│                │ weknora_credentials / provisioning_jobs / │
│                │ audit_events                              │
└──────────────────────────┬──────────────────────────────────┘
                           │  HTTP /api/v1 + X-API-Key   私网
                           ▼
┌─────────────────────────────────────────────────────────────┐
│  WeKnora（不暴露公网，仅 Wrapper 可访问）                   │
│  Gin Router + Auth(JWT|APIKey) + Handlers + Services        │
│  PostgreSQL / Redis / Vector DB / DocReader …               │
└─────────────────────────────────────────────────────────────┘
```

### 4.2 关键运行时数据流

#### 4.2.1 创建外部租户
```
外部调用方 → POST /v1/tenants
  ├─ 包装层鉴权（client_id/secret）
  ├─ Provisioner.Begin(idempotency_key)            ← 幂等性
  ├─ 生成合成 owner email/password
  ├─ HTTP POST WeKnora /api/v1/auth/register       ← 创建 user + tenant
  ├─ HTTP POST WeKnora /api/v1/auth/login          ← 取得 tenant.APIKey 明文
  ├─ 加密 APIKey → INSERT weknora_credentials
  ├─ INSERT wrapper_tenants (status=active)
  ├─ Provisioner.Commit
  └─ 返回 wrapper_tenant_id（永不返回 weknora_tenant_id 或 APIKey）
```

#### 4.2.2 RAG 对话（SSE）
```
外部 → POST /v1/chat/rag/:session_id
  ├─ 解析包装层身份 → resolve(wrapper_tenant_id → weknora_tenant_id, APIKey)
  ├─ reverseProxy.Director:
  │    req.URL = WeKnora /api/v1/knowledge-chat/:session_id
  │    req.Header.Set("X-API-Key", apikey)
  │    req.Header.Del("Authorization")
  ├─ reverseProxy.FlushInterval = -1            ← SSE 每写即刷
  ├─ 透传 Content-Type: text/event-stream
  ├─ 客户端断开 → ctx cancel → 上游中断
  └─ 写审计（不写 query 内容明文，写 hash）
```

#### 4.2.3 Multipart 文件上传
```
外部 → POST /v1/knowledge-bases/:kb_id/files
  ├─ 限制 Content-Length（避免上游 OOM）
  ├─ 直接流式转发 body 到 WeKnora /api/v1/knowledge-bases/:kb_id/knowledge/file
  ├─ 注入 X-API-Key，保留 boundary
  └─ 转发响应原样回客户端
```

### 4.3 文件 URL 策略（D4）

**推荐"重新签名"**：
- 客户端拿到的是包装层路径，如 `/v1/files/:wrapper_file_id?sig=...&expires=...`
- 包装层维护 `wrapper_file_id → weknora_file_path` 映射
- 包装层签名 HMAC 与 WeKnora 的 HMAC **完全独立**，密钥不复用
- WeKnora 的 `/api/v1/files/presigned` 在包装层后端**仅内部使用**

**透传方案的代价**：泄露 WeKnora `tenant_id`、内部文件路径、内部签名格式；后续 WeKnora 文件路径或签名算法升级会破坏调用方。

### 4.4 与跨租户能力的关系

WeKnora 的 `EnableCrossTenantAccess` + `CanAccessAllTenants` 双重门控**默认不开启**。包装层**不依赖**该能力：
- 每个 wrapper_tenant 各自持有独立 WeKnora APIKey，天然隔离
- 包装层不再需要 WeKnora 层的"管理员"概念
- 若运维需要后台批量管理，可单独建一个"运维 wrapper_tenant"加白名单跨包装层访问，仍**不**触碰 WeKnora 的跨租户开关

### 4.5 与 WeKnora `/auth/register` 的依赖

包装层开通流程必须依赖 `POST /api/v1/auth/register`（在 WeKnora 中默认免认证、未受白名单限制）。**部署前置条件**：
- 该端点在生产网络中**仅对包装层可达**（私网或防火墙白名单）
- 若运维侧禁用了 `/auth/register`，备选方案：以系统管理员账号（开启跨租户）调用 `POST /api/v1/tenants` 直接建租户

---

## 5. 已排除的混合方案备忘

| 想法 | 不采用原因 |
|---|---|
| 在 WeKnora 内 import 一个外部包注册路由 | 仍需改 `cmd/server/main.go`，违反 C1 |
| 在 WeKnora 容器 entrypoint 上拦截 | 需要重新打镜像或挂卷覆盖文件，等价 fork |
| 用 OpenTelemetry / Langfuse 中间件 hook | 这些是 observability 钩子，不是业务路由扩展点 |
| 用 MCP Server 暴露 | MCP 本质是 LLM 工具协议，不适合做业务 API gateway |

---

## 6. 后续阅读

- 端点级映射 → [02-api-mapping.md](./02-api-mapping.md)
- 数据库 schema → [03-data-model.md](./03-data-model.md)
- 部署拓扑 → [04-deployment.md](./04-deployment.md)
- 分阶段开发任务 → [05-development-plan.md](./05-development-plan.md)
