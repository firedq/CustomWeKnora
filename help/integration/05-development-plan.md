# 开发实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **本计划尚未启动开发**。每个阶段开始前需用户复核 → Y 后方可推进；任何对 WeKnora **源码**的修改请求必须**立即叫停**并回到 `00-overview.md` 修订集成方式。

**Goal:** 构建独立 Go BFF/Gateway 服务，在不改动 WeKnora 源码的前提下对调用方暴露完整生命周期 API（租户 / 用户 / 知识库 / 检索 / 对话 / 文件）。

**Architecture:** 独立进程 + 独立 DB，HTTP 反代到 WeKnora `/api/v1`，注入 `X-API-Key`。SSE / multipart 全程流式。详见 [01-architecture.md](./01-architecture.md)。

**Tech Stack:** Go 1.22+ · Gin · `net/http/httputil`（反代）· PostgreSQL 14+ · `golang-migrate` · `golang-jwt/jwt/v5` · `crypto/aes` + KMS · Redis（限流，可选）· OpenTelemetry · Prometheus · testify。

**仓库位置（决策 D2）:** **独立仓库 `weknora-wrapper/`**。本计划中所有路径以独立仓库根为基准。WeKnora 仓库**只读**。

---

## 阶段 0：前置确认（人工，无代码动作）

**目标**：在 Phase 1 集成文档全部走查通过后，确认 6 个关键决策。

- [ ] **D1 语言**：默认 Go。如需变更，回到 `00-overview.md` 重新讨论
- [ ] **D2 位置**：默认独立仓库 `weknora-wrapper/`
- [ ] **D3 用户语义**：默认包装层独立用户，多用户共享一个 WeKnora 租户
- [ ] **D4 文件 URL**：默认重新签名
- [ ] **D5 WeKnora 注册接口**：确认生产可被包装层访问；若禁用，规划备用开通方案
- [ ] **D6 文件代理路径**：默认 `/v1/files/:id?sig=...`

输出：在 `00-overview.md` 中将"推荐默认"打勾即可。

---

## 阶段 1：脚手架与最小可运行（Day 1-2）

### Task 1.1 仓库与项目骨架

**Files:**
- Create: `weknora-wrapper/go.mod`
- Create: `weknora-wrapper/cmd/api/main.go`
- Create: `weknora-wrapper/internal/config/config.go`
- Create: `weknora-wrapper/internal/http/router/router.go`
- Create: `weknora-wrapper/internal/http/handlers/health.go`
- Create: `weknora-wrapper/deployments/docker-compose.yml`
- Create: `weknora-wrapper/README.md`

- [ ] **Step 1.1.1** 初始化 `go mod init github.com/<org>/weknora-wrapper`
- [ ] **Step 1.1.2** 引入依赖：`gin-gonic/gin`、`spf13/viper`、`spf13/cobra`（可选）
- [ ] **Step 1.1.3** 写 `cmd/api/main.go`：加载配置、注册路由、`r.Run(":8080")`
- [ ] **Step 1.1.4** 实现 `GET /v1/health` → `{"status":"ok"}`（同时探活 WeKnora `/health`）
- [ ] **Step 1.1.5** `docker-compose up` 验证容器启动，`curl /v1/health` 通
- [ ] **Step 1.1.6** Commit: `feat: scaffold wrapper bff with /v1/health`

### Task 1.2 配置加载

**Files:**
- Modify: `weknora-wrapper/internal/config/config.go`
- Create: `weknora-wrapper/configs/config.example.yaml`

- [ ] **Step 1.2.1** 定义 `Config` 结构（参考 `04-deployment.md` §3）
- [ ] **Step 1.2.2** 用 viper 支持 yaml + env 双源
- [ ] **Step 1.2.3** 单元测试：缺字段返回明确错误
- [ ] **Step 1.2.4** Commit

---

## 阶段 2：WeKnora HTTP 客户端 + 反代基座（Day 3-4）

### Task 2.1 WeKnora Adapter Client

**Files:**
- Create: `weknora-wrapper/internal/weknora/client.go`
- Create: `weknora-wrapper/internal/weknora/auth.go`
- Create: `weknora-wrapper/internal/weknora/tenants.go`
- Create: `weknora-wrapper/internal/weknora/errors.go`
- Test:   `weknora-wrapper/internal/weknora/*_test.go`

- [ ] **Step 2.1.1** `Client` 结构：`http.Client` + `BaseURL` + 缓存的 retry policy
- [ ] **Step 2.1.2** 实现 `Register(email, password)` → `POST /api/v1/auth/register`
- [ ] **Step 2.1.3** 实现 `Login(email, password)` → `POST /api/v1/auth/login` 返回 `token` + 通过 `GET /api/v1/auth/me` 取 `tenant.api_key`（或解析 register/login 响应中已含的 tenant 对象）
- [ ] **Step 2.1.4** 实现 `ResetAPIKey(tenantID, jwt)` → `POST /api/v1/tenants/:id/api-key`
- [ ] **Step 2.1.5** 实现统一错误包装 `weknora.Error`（保留上游 HTTP code + 上游 `message`）
- [ ] **Step 2.1.6** 用 `httptest.NewServer` mock WeKnora，写 register/login/reset_api_key 单测
- [ ] **Step 2.1.7** Commit

### Task 2.2 流式反代基座

**Files:**
- Create: `weknora-wrapper/internal/proxy/reverse_proxy.go`
- Create: `weknora-wrapper/internal/proxy/sse.go`
- Create: `weknora-wrapper/internal/proxy/multipart.go`
- Test:   `weknora-wrapper/internal/proxy/*_test.go`

- [ ] **Step 2.2.1** 封装 `httputil.ReverseProxy`，注入 Director：替换 host、注入 `X-API-Key`、删除 `Authorization`
- [ ] **Step 2.2.2** SSE：`FlushInterval = -1`，响应头确认 `Cache-Control: no-cache`、`X-Accel-Buffering: no`
- [ ] **Step 2.2.3** Multipart：禁用 request buffering，限制最大 size，限制最大 part 数
- [ ] **Step 2.2.4** 单测：mock 上游分块输出，校验客户端**每写即收**而非一次性
- [ ] **Step 2.2.5** Commit

---

## 阶段 3：身份与凭证存储（Day 5-6）

### Task 3.1 DB Migrations

**Files:**
- Create: `weknora-wrapper/internal/store/migrations/0001_create_wrapper_tenants.up.sql`（含 `.down.sql`）
- Create: `weknora-wrapper/internal/store/migrations/0002_create_wrapper_users.up.sql`
- Create: `weknora-wrapper/internal/store/migrations/0003_create_weknora_credentials.up.sql`
- Create: `weknora-wrapper/internal/store/migrations/0004_create_provisioning_jobs.up.sql`
- Create: `weknora-wrapper/internal/store/migrations/0005_create_wrapper_files.up.sql`
- Create: `weknora-wrapper/internal/store/migrations/0006_create_audit_events.up.sql`

- [ ] **Step 3.1.1** 按 `03-data-model.md` 落 schema
- [ ] **Step 3.1.2** 启动时通过 `golang-migrate` 自动执行
- [ ] **Step 3.1.3** 集成测试：`docker-compose` 起 PG → 启动 wrapper → 所有表存在
- [ ] **Step 3.1.4** Commit

### Task 3.2 Repository 层

**Files:**
- Create: `weknora-wrapper/internal/store/repositories/tenants.go`
- Create: `weknora-wrapper/internal/store/repositories/users.go`
- Create: `weknora-wrapper/internal/store/repositories/credentials.go`
- Create: `weknora-wrapper/internal/store/repositories/audit.go`
- Test:   每个文件配对的 `_test.go`

- [ ] **Step 3.2.1** 基于 `pgx` 或 `sqlx` 实现 CRUD（不引 ORM；保持显式）
- [ ] **Step 3.2.2** `credentials.go` 写入路径只接受密文，绝不暴露明文返回方法签名外
- [ ] **Step 3.2.3** 用 `testcontainers-go` 起真实 PG 跑集成测试（**绝不** mock DB）
- [ ] **Step 3.2.4** Commit

### Task 3.3 KMS / 加密

**Files:**
- Create: `weknora-wrapper/internal/crypto/sealer.go`
- Create: `weknora-wrapper/internal/crypto/local_kms.go`（开发用）
- Create: `weknora-wrapper/internal/crypto/aws_kms.go`（生产）
- Test:   `weknora-wrapper/internal/crypto/*_test.go`

- [ ] **Step 3.3.1** 定义接口 `Sealer.Seal(plaintext) (ciphertext, dekID, error)`、`Unseal(ciphertext, dekID)` 
- [ ] **Step 3.3.2** `local_kms` 用 AES-256-GCM + 文件密钥（开发用）
- [ ] **Step 3.3.3** `aws_kms` 走真实 KMS Encrypt/Decrypt
- [ ] **Step 3.3.4** Commit

---

## 阶段 4：租户开通（Day 7-8）

### Task 4.1 Provisioner

**Files:**
- Create: `weknora-wrapper/internal/provisioning/tenant_provisioner.go`
- Test:   `weknora-wrapper/internal/provisioning/tenant_provisioner_test.go`

- [ ] **Step 4.1.1** 实现 `Provisioner.CreateTenant(ctx, req)`：状态机 init → registering → logging_in → storing_key → done
- [ ] **Step 4.1.2** 幂等：以 `idempotency_key` 为 key 写 `provisioning_jobs`；并发用 PG advisory lock 互斥
- [ ] **Step 4.1.3** 失败重试：可配置最大次数；最终失败 wrapper_tenants 置 `status=failed`
- [ ] **Step 4.1.4** 集成测试：mock WeKnora 正常 / 重试 / 注册失败 / 登录失败 / 加密失败
- [ ] **Step 4.1.5** Commit

### Task 4.2 Handlers

**Files:**
- Create: `weknora-wrapper/internal/http/handlers/tenants.go`
- Modify: `weknora-wrapper/internal/http/router/router.go`

- [ ] **Step 4.2.1** `POST /v1/tenants` → Provisioner.CreateTenant
- [ ] **Step 4.2.2** `GET /v1/tenants/:id` / `PATCH /v1/tenants/:id` / `DELETE /v1/tenants/:id`
- [ ] **Step 4.2.3** `POST /v1/tenants/:id/api-key/rotate` → 调 WeKnora ResetAPIKey + 加密替换
- [ ] **Step 4.2.4** 端到端测试（包装层 + WeKnora + PG）
- [ ] **Step 4.2.5** Commit

### Task 4.3 Wrapper 用户

**Files:**
- Create: `weknora-wrapper/internal/http/handlers/users.go`
- Modify: `weknora-wrapper/internal/http/router/router.go`

- [ ] **Step 4.3.1** `POST /v1/tenants/:tid/users` 等 CRUD（只读写 wrapper_users）
- [ ] **Step 4.3.2** 角色校验中间件（owner / admin / editor / viewer）
- [ ] **Step 4.3.3** Commit

---

## 阶段 5：知识库 + 知识条目（Day 9-11）

### Task 5.1 鉴权中间件

**Files:**
- Create: `weknora-wrapper/internal/http/middleware/auth.go`
- Create: `weknora-wrapper/internal/auth/jwt.go`

- [ ] **Step 5.1.1** 解析 wrapper JWT / wrapper API Key → 上下文注入 `wrapper_tenant_id`、`wrapper_user_id`、`role`
- [ ] **Step 5.1.2** 注入 WeKnora APIKey（解密后放上下文 `*WeKnoraCreds`，请求结束清理）
- [ ] **Step 5.1.3** 单测：缺 token / 错 token / token tenant 不存在
- [ ] **Step 5.1.4** Commit

### Task 5.2 KB Handlers

**Files:**
- Create: `weknora-wrapper/internal/http/handlers/knowledge_bases.go`
- Modify: `weknora-wrapper/internal/http/router/router.go`

- [ ] **Step 5.2.1** `POST/GET/PATCH/DELETE /v1/knowledge-bases` 全部用 reverseProxy
- [ ] **Step 5.2.2** 标签 CRUD（包装层路径 `/v1/knowledge-bases/:id/tags`）
- [ ] **Step 5.2.3** 异步任务进度端点透传
- [ ] **Step 5.2.4** 集成测试（真实 WeKnora）
- [ ] **Step 5.2.5** Commit

### Task 5.3 知识条目 Handlers

**Files:**
- Create: `weknora-wrapper/internal/http/handlers/knowledge.go`

- [ ] **Step 5.3.1** `POST /v1/knowledge-bases/:id/knowledge/file`（multipart 流式）
- [ ] **Step 5.3.2** `POST .../url` 与 `.../manual`
- [ ] **Step 5.3.3** 知识 CRUD、batch、reparse、move、search
- [ ] **Step 5.3.4** 集成测试：上传 PDF 验证从包装层到 WeKnora 全链路
- [ ] **Step 5.3.5** Commit

### Task 5.4 检索 Handlers

**Files:**
- Create: `weknora-wrapper/internal/http/handlers/search.go`

- [ ] **Step 5.4.1** `POST /v1/knowledge-bases/:id/hybrid-search`（把 GET+body 上游改 POST）
- [ ] **Step 5.4.2** `POST /v1/search` → `POST /api/v1/knowledge-search`
- [ ] **Step 5.4.3** 集成测试
- [ ] **Step 5.4.4** Commit

---

## 阶段 6：会话与 SSE 对话（Day 12-13）

### Task 6.1 Session Handlers

**Files:**
- Create: `weknora-wrapper/internal/http/handlers/sessions.go`

- [ ] **Step 6.1.1** Session CRUD 全部透传
- [ ] **Step 6.1.2** generate-title / pin / unpin / batch / stop
- [ ] **Step 6.1.3** 集成测试
- [ ] **Step 6.1.4** Commit

### Task 6.2 SSE Chat Handlers

**Files:**
- Create: `weknora-wrapper/internal/http/handlers/chat.go`

- [ ] **Step 6.2.1** `POST /v1/chat/rag/:sid` → `/api/v1/knowledge-chat/:session_id`
- [ ] **Step 6.2.2** `POST /v1/chat/agent/:sid` → `/api/v1/agent-chat/:session_id`
- [ ] **Step 6.2.3** `GET /v1/chat/:sid/stream` → `/api/v1/sessions/continue-stream/:session_id`
- [ ] **Step 6.2.4** SSE 集成测试：发问题、读到 `agent_thinking` / `agent_final_answer` / `agent_complete` 事件、客户端断开后上游中断
- [ ] **Step 6.2.5** Commit

### Task 6.3 Message Handlers

- [ ] **Step 6.3.1** 历史加载 / 删除 / 搜索透传
- [ ] **Step 6.3.2** Commit

---

## 阶段 7：文件代理 + 重新签名（Day 14-15）

### Task 7.1 wrapper_files 映射

**Files:**
- Create: `weknora-wrapper/internal/store/repositories/files.go`
- Create: `weknora-wrapper/internal/files/signer.go`
- Create: `weknora-wrapper/internal/http/handlers/files.go`

- [ ] **Step 7.1.1** 在知识详情 / 知识列表 / 分块响应解析后**透明插桩**：检测 `provider://...` URL → 写入/获取 `wrapper_files` → 替换为 `/v1/files/:wrapper_file_id?sig=...&expires=...`
- [ ] **Step 7.1.2** `signer.Sign(file_id, tenant_id, expires)` HMAC-SHA256，secret 独立
- [ ] **Step 7.1.3** `GET /v1/files/:id` 校验签名 → 反代 WeKnora `/files?file_path=...`
- [ ] **Step 7.1.4** 集成测试：上传图片 → 列出 → 拿到包装层签名 URL → 用该 URL 下载
- [ ] **Step 7.1.5** Commit

---

## 阶段 8：审计 / 限流 / 可观测（Day 16-17）

### Task 8.1 审计

- [ ] **Step 8.1.1** middleware 在每个 handler 后写 `audit_events`
- [ ] **Step 8.1.2** body / query / api_key 全部脱敏
- [ ] **Step 8.1.3** Commit

### Task 8.2 限流

- [ ] **Step 8.2.1** Redis token bucket，按 tenant + endpoint
- [ ] **Step 8.2.2** 默认 chat 60 RPM、upload 30 RPM、其余 600 RPM（可配置）
- [ ] **Step 8.2.3** Commit

### Task 8.3 可观测

- [ ] **Step 8.3.1** OpenTelemetry：HTTP middleware 自动埋点 + WeKnora client 透传 traceparent
- [ ] **Step 8.3.2** Prometheus `/metrics`：QPS、延迟分位、上游 status_code、SSE 活跃连接、key 解密耗时
- [ ] **Step 8.3.3** 结构化日志 + `request_id` 透传
- [ ] **Step 8.3.4** Commit

---

## 阶段 9：契约测试套件（Day 18-19）

> 这是包装层的**长期资产**。每次 WeKnora 升级先跑这套。

### Task 9.1 契约测试

**Files:**
- Create: `weknora-wrapper/tests/contract/tenants_test.go`
- Create: `weknora-wrapper/tests/contract/knowledge_test.go`
- Create: `weknora-wrapper/tests/contract/chat_test.go`
- Create: `weknora-wrapper/tests/contract/files_test.go`

- [ ] **Step 9.1.1** docker-compose 启动 WeKnora + Wrapper
- [ ] **Step 9.1.2** 自动化测试用例：create_tenant → create_kb → upload_file → search → rag_chat (SSE) → file_download → rotate_api_key → delete_tenant
- [ ] **Step 9.1.3** CI 上每次 PR 自动跑
- [ ] **Step 9.1.4** Commit

---

## 阶段 10：交付 / 文档（Day 20）

### Task 10.1 OpenAPI / SDK

- [ ] **Step 10.1.1** 生成 `docs/openapi.yaml`
- [ ] **Step 10.1.2** 生成 Go / TypeScript SDK
- [ ] **Step 10.1.3** Commit

### Task 10.2 部署文档

- [ ] **Step 10.2.1** docker-compose 生产示例
- [ ] **Step 10.2.2** k8s helm chart
- [ ] **Step 10.2.3** Commit

### Task 10.3 PR / Code Review（强制）

> 按全局 CLAUDE.md Phase 5 要求：必须调用 Codex + Gemini 双模型审计。

- [ ] **Step 10.3.1** 调用 Codex 做全量 code review，输出 Unified Diff 修复建议
- [ ] **Step 10.3.2** 调用 Gemini 做同样审计（针对 UI 涉及前端 SDK / OpenAPI 渲染部分）
- [ ] **Step 10.3.3** 整合修复
- [ ] **Step 10.3.4** （如果包装层放在 WeKnora 仓库内）刷新 graphify 图谱
- [ ] **Step 10.3.5** 提交最终 PR

---

## 风险登记表

| # | 风险 | 触发条件 | 缓解 |
|---|---|---|---|
| R1 | WeKnora `/auth/register` 在生产被禁用 | 部署时发现 | 准备跨租户管理员 + `POST /tenants` 备用通道；阶段 0 验证 |
| R2 | WeKnora `tenant.APIKey` 加密策略变更 / `CreateTenant` 不再返回明文 | WeKnora 版本升级 | 契约测试早发现；阶段 9 守护 |
| R3 | SSE 上游 keepalive 超时切断 | 长对话场景 | 上游配置 + 包装层 heartbeat 检查 |
| R4 | Multipart 上游大小限制不同步 | 上游改默认值 | 包装层做主限制，上游报错则回带 message |
| R5 | KMS 故障 | KMS 不可用 | API Key 解密带缓存（短 TTL），降级期间用缓存继续工作；KMS 恢复后强制 re-wrap |
| R6 | WeKnora `/api/v1/...` 路径或 body schema 变更 | 上游升级 | 契约测试 + 适配器层抽象 |
| R7 | 包装层 DB 与 WeKnora DB 数据不一致（如 WeKnora 直接删租户） | 运维误操作 | 不允许直接操作 WeKnora DB；包装层定期对账（轻量） |
| R8 | 多用户共享 owner 导致 WeKnora 内部按用户聚合困难 | 业务要求统计 | 在 session.metadata 中携带 external_user_key，包装层做二次聚合 |

---

## Self-Review Check（计划自查）

| 检查项 | 结果 |
|---|---|
| 02-api-mapping.md 中每个端点是否都对应到本计划阶段 5/6/7 的 Task | ✅ 已覆盖 KB / 知识 / 检索 / 会话 / SSE / 文件 |
| 03-data-model.md 中每张表是否都在阶段 3.1 落 migration | ✅ 0001-0006 对应 6 张表 |
| 04-deployment.md 中所有部署需求是否在阶段 10 覆盖 | ✅ docker-compose + k8s + OpenAPI |
| 是否所有 step 都是 2-5 分钟、可独立 commit | ✅ |
| 是否避免 placeholder（"TODO" / "类似上面" / "添加合适错误处理"） | ✅ |
| Type 一致性（e.g. `Provisioner.CreateTenant` 在阶段 4 / 阶段 9 命名一致） | ✅ |
