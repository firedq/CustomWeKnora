# 能力覆盖矩阵与上游版本跟进

> 回答两个常见疑问：
> 1. WeKnora 的哪些能力**可以 / 不能**通过包装层暴露给调用方？
> 2. 当 WeKnora 升级、新增能力时，包装层如何**快速跟进**让下游同步获得？
>
> 本文锁定包装层"端点开放策略"与"上游变更响应 SOP"两条治理规则。

---

## 第一部分 · 端点覆盖矩阵

### 1. 四档分类

| 档位 | 含义 | 默认策略 |
|---|---|---|
| **A** | 业务数据面 | 默认开放，普通用户角色可调 |
| **B** | 租户配置面 | 仅 `admin` 角色，挂在 `/v1/admin/*` 路径 |
| **C** | 鉴权 / 运维 / IM 等 | **不暴露**，由包装层或部署运维内部使用 |
| **D** | 包装层加工后暴露 | 由包装层重写路径 / 重新签名 / 编排多步上游调用 |

约 **85%** 的 WeKnora 能力（A+B+D）可以经包装层暴露；剩余 **15%**（C）保留在运维 / 包装层内部。

---

### 2. A 类 · 业务数据面（默认开放）

**判断标准**：纯数据 CRUD / 检索 / 对话，租户隔离已由 `X-API-Key` 自然保证。

| WeKnora 路由前缀 | 域 |
|---|---|
| `/api/v1/knowledge-bases` | 知识库 |
| `/api/v1/knowledge-bases/:id/tags` | 标签 |
| `/api/v1/knowledge-bases/:id/knowledge/{file,url,manual}` | 知识入库 |
| `/api/v1/knowledge`、`/api/v1/knowledge/:id` | 知识详情 / 列表 / 更新 / 删除 / 批量操作 |
| `/api/v1/chunks` | 分块 |
| `/api/v1/knowledge-bases/:id/faq` | FAQ 知识库 |
| `/api/v1/knowledge-bases/:id/hybrid-search` | 混合检索 |
| `/api/v1/knowledge-search` | 跨库检索 |
| `/api/v1/sessions`、`/api/v1/messages` | 会话 / 消息 |
| `/api/v1/knowledge-chat/:sid`、`/api/v1/agent-chat/:sid`（SSE） | 对话 |
| `/api/v1/sessions/continue-stream/:sid`（SSE） | 断线续传 |
| `/api/v1/knowledgebase/:kb_id/wiki/*` | Wiki / 知识图谱 |

完整端点映射见 [02-api-mapping.md](./02-api-mapping.md) §3-§10。

---

### 3. B 类 · 租户配置面（管理员角色门控）

**判断标准**：能转发但会改变租户级行为，普通业务调用方不该触达。

| WeKnora 路由 | 风险点 | 推荐包装层路径 |
|---|---|---|
| `/api/v1/tenants/kv/:key` | 改租户级开关 | `/v1/admin/tenant-config/:key` |
| `/api/v1/models/*` | 增删 LLM / Embedding / Rerank 模型 | `/v1/admin/models/*` |
| `/api/v1/vector-stores/*` | 切换向量库存储引擎 | `/v1/admin/vector-stores/*` |
| `/api/v1/web-search-providers/*` | Web Search 外部凭证 | `/v1/admin/web-search-providers/*` |
| `/api/v1/mcp-services/*` | MCP 工具接入 | `/v1/admin/mcp-services/*` |
| `/api/v1/datasource/*` | 外部数据源同步凭证 | `/v1/admin/datasources/*` |
| `/api/v1/agents/*`、`/api/v1/skills` | Custom Agent / Skill | `/v1/admin/agents/*` |
| `/api/v1/organizations/*`、`/api/v1/knowledge-bases/:id/shares`、`/api/v1/agents/:id/shares` | 跨租户共享（与一外部租户一 WeKnora 租户语义冲突，需单独设计） | `/v1/admin/organizations/*`（默认关闭） |
| `/api/v1/evaluation/*` | 评估调试 | `/v1/admin/evaluation/*` |

**门控实现**：包装层 `middleware.RequireAdmin` 检查 `wrapper_users.role ∈ {owner, admin}`，否则 403。

---

### 4. C 类 · 不暴露（鉴权 / 运维 / 协议冲突）

| WeKnora 路由 | 不暴露原因 |
|---|---|
| `/api/v1/auth/register`、`/auth/login`、`/auth/refresh`、`/auth/logout`、`/auth/me`、`/auth/oidc/*`、`/auth/change-password`、`/auth/validate`、`/auth/auto-setup` | 包装层有独立鉴权体系。WeKnora 的 JWT 一旦下放，包装层无法回收、轮换、审计 |
| `/api/v1/tenants`（POST）、`/api/v1/tenants/:id/api-key`（POST） | 必须由 Provisioner 编排（加密落库），**绝不**让客户端直接调；包装层用 `/v1/tenants` + `/v1/tenants/:id/api-key/rotate` 包一层 |
| `/api/v1/tenants/all`、`/api/v1/tenants/search` | WeKnora 跨租户管理员视角，包装层永远关闭 |
| `/api/v1/initialization/*` | Ollama 探测、embedding/rerank/ASR 体检、KB 初始化、文本抽取测试。一次性引导，部署 / 运维流程使用 |
| `/api/v1/system/*` | 系统诊断（parser 引擎、存储状态、docreader 重连）。运维面 |
| `/api/v1/weknoracloud/*` | WeKnora 接入 WeKnora Cloud 的厂商凭证存取，纯内部 |
| `/api/v1/im/callback/:channel_id`、`/api/v1/im-channels/*`、`/api/v1/wechat/*` | IM 回调使用各平台自身签名验证（注册在 Auth 中间件**前**），鉴权链与包装层冲突；IM Channel 配置语义需特殊设计才能映射到外部租户 |
| `/api/v1/chunker/preview` | 调试用 |
| `/api/v1/files/presigned` | 已由 D 类重新签名替代 |
| `/swagger/*` | 内部 API 文档，不对调用方 |
| `/health` | 包装层有自己的 `/v1/health` |

---

### 5. D 类 · 包装层加工后暴露

**判断标准**：上游能力本身需要，但形态/鉴权/凭证需要包装层先重写。

| WeKnora 路由 | 包装层处理 |
|---|---|
| `GET /files?file_path=...` | 重新签名 → `GET /v1/files/:id?sig=...&expires=...`；映射通过 `wrapper_files` 表（见 [03-data-model.md](./03-data-model.md) §2.5） |
| `POST /api/v1/tenants`（创建） | Provisioner 编排：`register` → `login` → 加密落库 |
| `POST /api/v1/tenants/:id/api-key`（轮换） | Provisioner 编排：ResetAPIKey → 加密替换 → 缓存失效 |
| `GET /api/v1/knowledge/:id/download` & `/preview` | 透传同时附加业务审计、可选水印 |

---

### 6. 覆盖率速查

```
WeKnora 路由总览（按域）
├─ Auth(11)          → C 全部不暴露
├─ Tenant(8)         → 4 个 A/B（GET/PATCH/DELETE/KV），2 个 D（Create/RotateKey），2 个 C（all/search）
├─ KnowledgeBase(10) → 全 A
├─ KnowledgeTag(4)   → 全 A
├─ Knowledge(15)     → 全 A
├─ Chunk(6)          → 全 A
├─ FAQ(10)           → 全 A
├─ Search(3)         → 全 A
├─ Session(11)       → 全 A
├─ Message(4)        → 全 A
├─ Chat SSE(3)       → 全 A
├─ Model(6)          → 全 B
├─ VectorStore(7)    → 全 B
├─ WebSearch(8)      → 全 B
├─ MCPService(10)    → 全 B
├─ DataSource(13)    → 全 B
├─ CustomAgent(8)    → 全 B
├─ Skill(1)          → 全 B
├─ Organization(20)  → 全 B（默认关闭）
├─ Evaluation(2)     → 全 B
├─ WikiPage(13)      → 全 A
├─ Initialization(14)→ 全 C
├─ System(6)         → 全 C
├─ WeKnoraCloud(2)   → 全 C
├─ IM(4)             → 全 C
├─ ChunkerDebug(1)   → 全 C
└─ Files(2)          → 全 D

约：A 87 个 / B 75 个 / C 38 个 / D 6 个   覆盖比例 ≈ 84%
```

数字会随上游版本变化，以包装层 `02-api-mapping.md` 实际登记为准。

---

## 第二部分 · 上游版本跟进 SOP

### 7. 版本对齐声明

包装层 `README.md` 与 `go.mod` 同级强制声明：

```
Supported WeKnora version: v0.5.x
Tested against: v0.5.0, v0.5.1, v0.5.2
Compatibility: WeKnora minor versions within v0.5.x are tested compatible.
               Major version bumps (v0.6.x +) require full re-validation.
```

- 包装层每个 release tag 绑定一个**已通过契约测试**的 WeKnora 版本集合
- 上游主版本变化默认视为 **Breaking**，触发完整流程
- 上游小版本变化默认视为 **Compatible**，仅跑契约测试

---

### 8. 五步跟进流程

```
WeKnora 新版本发布
        │
        ▼
[Step 1] Diff 上游变更
        - WeKnora CHANGELOG.md
        - git diff vX.Y.Z..vA.B.C -- internal/router/ internal/handler/
        - swagger.json diff
        │
        ▼
[Step 2] 端点变更分类
        A 类 新增端点    → "快速通道" 或正式包装
        B 类 修改 schema → 适配器层改一处
        C 类 删除/重命名 → 保留 Deprecation
        D 类 鉴权变化    → 必须人工 review
        │
        ▼
[Step 3] 更新 02-api-mapping.md
        - 一行一变更
        - 标 since/until 字段
        │
        ▼
[Step 4] 跑契约测试套件（05-development-plan.md 阶段 9 资产）
        - tenants / knowledge / chat (SSE) / files 四组
        - 失败 → 修适配器；成功 → Step 5
        │
        ▼
[Step 5] 灰度发布
        - 一个 wrapper 副本指向新 WeKnora
        - 与生产副本对比响应
        - 通过 → 全量切；不通过 → 回滚 + 修复
```

每步必须有产出物（diff 报告 / 分类表 / mapping 改动 / 测试结果 / 灰度结论），无产出物视为该步未完成。

---

### 9. 适配器是唯一耦合点

包装层目录里只有 **`internal/weknora/`** 这一层接触 WeKnora API 形状。

```
internal/weknora/
├── client.go         HTTP 客户端 + 重试 + 头注入
├── tenants.go        1 个文件 ↔ WeKnora 1 个资源域
├── knowledge_bases.go
├── knowledge.go
├── sessions.go
├── chat_proxy.go
├── files_proxy.go
├── (新增) wiki.go    上游新增域时新增文件，不动旧文件
└── ...
```

**Handler 层（`internal/http/handlers/`）通过 interface 依赖 adapter，永不直连 HTTP**。

```go
// internal/weknora/knowledge_bases.go
type KBAdapter interface {
    Create(ctx, req CreateReq) (KB, error)
    Get(ctx, id string) (KB, error)
    // ...
}

// internal/http/handlers/knowledge_bases.go
type KBHandler struct {
    adapter KBAdapter  // ← 只看接口，不知道 HTTP 在哪
}
```

这样：

- WeKnora schema 变 → 改 1 个 adapter 文件 / 测试不动 / Handler 不动 / 对外 API 不动
- 包装层对外 API 形状 → 保持稳定，**调用方业务系统永远无感知**

---

### 10. 快速通道：`/v1/passthrough/*`

针对"WeKnora 刚加了新能力，希望本周就让调用方用上，不想立刻写完整包装"的场景，包装层提供通用代理端点：

```
ANY /v1/passthrough/*upstream_path
```

- **角色门控**：仅 wrapper `admin` 角色及以上可调用
- **凭证注入**：仍由包装层注入 `X-API-Key`（凭证不外泄）
- **审计**：默认开启，每次调用写 `audit_events`，`action='passthrough'`
- **限流**：默认 60 RPM
- **白名单**：通过配置文件控制可经此通道触达的 WeKnora 路径（如允许 `/api/v1/knowledge-bases/*`，禁止 `/api/v1/tenants/*` 与 `/auth/*`、`/system/*`）
- **生命周期**：每个 passthrough 路径加入后，**两周内**必须晋升为正式 `/v1/...` 路由，否则告警

**严禁**用作长期方案。passthrough 流量持续超过两周需触发包装层维护者的整改工单。

---

### 11. 自动化辅助

| 工具 | 用途 | 集成位置 |
|---|---|---|
| GitNexus / Graphify（本仓库已有） | 比较新旧 WeKnora 版本的服务图谱、新增/删除符号 | 升级前手动跑 |
| `scripts/diff_router.sh`（包装层自带） | 跑 `git diff` 检测 `internal/router/` 变化，输出新增/修改路由清单 | CI 上 nightly |
| Schemathesis | 基于上游 swagger 自动 fuzz | 包装层 CI 中针对 staging WeKnora 跑 |
| 契约测试套件 | 端到端核心场景：create_tenant → upload → search → chat → download | CI 每次 PR + nightly |

---

### 12. 治理责任划分

| 角色 | 责任 |
|---|---|
| WeKnora 上游维护者 | 维护 `CHANGELOG.md`，标注 Breaking |
| 包装层维护者 | 接收变更工单 → 按五步流程 → 更新 `02-api-mapping.md` |
| 调用方业务系统 | 仅通过包装层 OpenAPI 感知能力，**不**直接读 WeKnora 文档 |

---

### 13. 适配成本预估

| 上游变更类型 | 包装层工作量 |
|---|---|
| 新增纯转发端点 | passthrough 即用（≤ 1 小时）；正式化 2-4 小时 |
| 修改已有端点 body schema（向下兼容） | ≤ 30 分钟（改一个 struct） |
| 修改已有端点路径 | 1-2 小时（adapter URL 改，包装层路径保留） |
| 新增整域（如新增 `/api/v1/foo/*`） | 半天 ~ 1 天（新增 1 个 adapter 文件 + 1 个 handler 文件 + 契约测试） |
| 鉴权机制变化 | 4-8 小时（需 review middleware） |
| 删除端点 | 包装层保留 Deprecation 警告 + N 个月后下线 |

---

### 14. 反模式（禁止）

| 反模式 | 危害 |
|---|---|
| Handler 直连 WeKnora HTTP，跳过 adapter | 升级时找不到所有耦合点 |
| 把 WeKnora 错误对象直接返回给调用方 | 上游 error 形状变化即破坏调用方 |
| 用 `interface{}` 透传 WeKnora 响应体 | schema drift 时无类型检查保护 |
| 在调用方文档中引用 WeKnora 端点 | 调用方会绕过包装层直连 WeKnora |
| 长期使用 passthrough | 失去封装价值，等同方案 D |

---

## 第三部分 · 速记卡

```
能不能转发？
    A 业务数据面 → 默认开放
    B 租户配置面 → admin 角色 + /v1/admin/*
    C 鉴权/运维  → 不暴露
    D 加工后     → 包装层重写

上游升级怎么跟？
    Step 1 Diff CHANGELOG + router
    Step 2 分类 A/B/C/D
    Step 3 更新 02-api-mapping.md
    Step 4 跑契约测试
    Step 5 灰度发布

哪里改？
    永远只动 internal/weknora/  ← 唯一耦合点
    Handler / Mapping / 对外 API → 不动
```
