# 包装层 ↔ WeKnora 端点映射

> 给出包装层每个对外端点对应的 WeKnora 上游端点、注入的请求头、关键转换与注意事项。

---

## 1. 通用约定

### 1.1 请求头
| 头 | 调用方 → 包装层 | 包装层 → WeKnora |
|---|---|---|
| 鉴权 | `Authorization: Bearer <wrapper_jwt>` 或 `X-Wrapper-API-Key: <key>` | `X-API-Key: <weknora_tenant_apikey>` |
| 请求追踪 | `X-Request-ID`（透传或生成） | 透传同名 |
| Content-Type | JSON / multipart / SSE | 原样透传 |
| 客户标识 | `X-Client-Id` | 不透传 |

### 1.2 响应包络
WeKnora 返回 `{"success": bool, "data": ...}` 或错误 `{"success": false, "message": ..., "code": ...}`。包装层**保留**该形态，但增加包装层错误码：
```json
{
  "success": false,
  "code": "WRAPPER_INVALID_TENANT",
  "message": "...",
  "request_id": "..."
}
```

### 1.3 鉴权策略
- 包装层独立鉴权（JWT 或包装层 API Key）→ 解出 `wrapper_tenant_id` 与 `wrapper_user_id`
- 查 `weknora_credentials` 表取明文 APIKey（KMS 解密）→ 注入 `X-API-Key`
- WeKnora 的 JWT/Bearer 模式**不暴露**给调用方

### 1.4 标记
| 标记 | 含义 |
|---|---|
| 🔒 | 仅包装层管理员可调用（如租户开通） |
| 🌊 | SSE 流式 |
| 📦 | multipart |
| 🔗 | 文件代理 / 重新签名 |
| ⏱ | 异步任务，返回 `task_id` |

---

## 2. 租户管理

| 包装层 | WeKnora 上游 | 说明 |
|---|---|---|
| 🔒 `POST /v1/tenants` | `POST /api/v1/auth/register` + `POST /api/v1/auth/login` | 合成 owner，登录后取 APIKey 加密落库 |
| `GET /v1/tenants/:id` | 仅查包装层 DB（必要时验证 `GET /api/v1/tenants/:weknora_id`） | 永不返回 `weknora_tenant_id` / APIKey |
| `PATCH /v1/tenants/:id` | `PUT /api/v1/tenants/:weknora_id` | 同步显示名、描述 |
| 🔒 `DELETE /v1/tenants/:id` | `DELETE /api/v1/tenants/:weknora_id` | 同步软删 wrapper_tenants |
| 🔒 `POST /v1/tenants/:id/api-key/rotate` | `POST /api/v1/tenants/:weknora_id/api-key` | 事务化：上游轮换 → 取新明文 → 加密落库 → 失效旧 key 缓存 |

> **包装层永不暴露** `GET /tenants/all`、`/tenants/search`（WeKnora 跨租户接口）给外部调用方。

---

## 3. 用户管理（包装层独立）

> 默认（决策 D3）：包装层用户多对一映射到同一 WeKnora 租户的合成 owner。**不**调用 WeKnora `/auth/register`。

| 包装层 | 上游 | 说明 |
|---|---|---|
| `POST /v1/tenants/:tid/users` | 仅 wrapper_users | 落库即可 |
| `GET /v1/tenants/:tid/users` | 仅 wrapper_users |  |
| `GET /v1/tenants/:tid/users/:uid` | 仅 wrapper_users |  |
| `PATCH /v1/tenants/:tid/users/:uid` | 仅 wrapper_users | 改角色 / 启停 |
| `DELETE /v1/tenants/:tid/users/:uid` | 仅 wrapper_users |  |
| `GET /v1/me` | 仅 wrapper_users | 当前包装层用户 |

> 角色（建议）：`owner` / `admin` / `editor` / `viewer`。  
> 包装层应在 Handler 中校验角色，再决定能否触达对应 WeKnora 操作。

---

## 4. 知识库管理

| 包装层 | WeKnora 上游 | 说明 |
|---|---|---|
| `POST /v1/knowledge-bases` | `POST /api/v1/knowledge-bases` | body 直传，注入 X-API-Key |
| `GET /v1/knowledge-bases` | `GET /api/v1/knowledge-bases` | 列表 |
| `GET /v1/knowledge-bases/:id` | `GET /api/v1/knowledge-bases/:id` |  |
| `PATCH /v1/knowledge-bases/:id` | `PUT /api/v1/knowledge-bases/:id` | 使用 PATCH 更符合 REST，body 透传 |
| `DELETE /v1/knowledge-bases/:id` | `DELETE /api/v1/knowledge-bases/:id` |  |
| `POST /v1/knowledge-bases/:id/pin` | `PUT /api/v1/knowledge-bases/:id/pin` | 置顶 |
| `POST /v1/knowledge-bases/copy` ⏱ | `POST /api/v1/knowledge-bases/copy` | 异步 |
| `GET /v1/knowledge-bases/copy/progress/:task_id` | `GET /api/v1/knowledge-bases/copy/progress/:task_id` |  |
| `GET /v1/knowledge-bases/:id/move-targets` | `GET /api/v1/knowledge-bases/:id/move-targets` |  |

### 4.1 标签
| 包装层 | 上游 |
|---|---|
| `GET /v1/knowledge-bases/:id/tags` | `GET /api/v1/knowledge-bases/:id/tags` |
| `POST /v1/knowledge-bases/:id/tags` | `POST /api/v1/knowledge-bases/:id/tags` |
| `PATCH /v1/knowledge-bases/:id/tags/:tag_id` | `PUT /api/v1/knowledge-bases/:id/tags/:tag_id` |
| `DELETE /v1/knowledge-bases/:id/tags/:tag_id` | `DELETE /api/v1/knowledge-bases/:id/tags/:tag_id` |

---

## 5. 知识条目（文档型）

| 包装层 | WeKnora 上游 | 备注 |
|---|---|---|
| 📦 `POST /v1/knowledge-bases/:id/knowledge/file` | `POST /api/v1/knowledge-bases/:id/knowledge/file` | 流式 multipart |
| `POST /v1/knowledge-bases/:id/knowledge/url` | `POST /api/v1/knowledge-bases/:id/knowledge/url` |  |
| `POST /v1/knowledge-bases/:id/knowledge/manual` | `POST /api/v1/knowledge-bases/:id/knowledge/manual` | 手工 Markdown |
| `GET /v1/knowledge-bases/:id/knowledge` | `GET /api/v1/knowledge-bases/:id/knowledge` | 支持分页 / 关键词 / tag |
| `DELETE /v1/knowledge-bases/:id/knowledge` ⏱ | `DELETE /api/v1/knowledge-bases/:id/knowledge` | 清空 |
| `GET /v1/knowledge/:id` | `GET /api/v1/knowledge/:id` |  |
| `GET /v1/knowledge/batch?ids=...` | `GET /api/v1/knowledge/batch` |  |
| `PATCH /v1/knowledge/:id` | `PUT /api/v1/knowledge/:id` |  |
| `PATCH /v1/knowledge/manual/:id` | `PUT /api/v1/knowledge/manual/:id` |  |
| `POST /v1/knowledge/:id/reparse` ⏱ | `POST /api/v1/knowledge/:id/reparse` |  |
| 🔗 `GET /v1/knowledge/:id/download` | `GET /api/v1/knowledge/:id/download` | 透传 Content-Type / Disposition |
| 🔗 `GET /v1/knowledge/:id/preview` | `GET /api/v1/knowledge/:id/preview` | inline |
| `DELETE /v1/knowledge/:id` | `DELETE /api/v1/knowledge/:id` |  |
| `POST /v1/knowledge/batch-delete` ⏱ | `POST /api/v1/knowledge/batch-delete` | ≤200 条 |
| `POST /v1/knowledge/move` ⏱ | `POST /api/v1/knowledge/move` |  |
| `GET /v1/knowledge/move/progress/:task_id` | `GET /api/v1/knowledge/move/progress/:task_id` |  |
| `GET /v1/knowledge/search?keyword=...` | `GET /api/v1/knowledge/search` | 文件列表搜索 |
| `PUT /v1/knowledge/tags` | `PUT /api/v1/knowledge/tags` | 批量打标签 |

### 5.1 分块管理（可选暴露）
| 包装层 | 上游 |
|---|---|
| `GET /v1/chunks/:knowledge_id` | `GET /api/v1/chunks/:knowledge_id` |
| `GET /v1/chunks/by-id/:id` | `GET /api/v1/chunks/by-id/:id` |
| `PATCH /v1/chunks/:knowledge_id/:id` | `PUT /api/v1/chunks/:knowledge_id/:id` |
| `DELETE /v1/chunks/:knowledge_id/:id` | `DELETE /api/v1/chunks/:knowledge_id/:id` |
| `DELETE /v1/chunks/:knowledge_id` | `DELETE /api/v1/chunks/:knowledge_id` |

---

## 6. FAQ 知识库（如启用）

| 包装层 | 上游 |
|---|---|
| `GET /v1/knowledge-bases/:id/faq/entries` | `GET /api/v1/knowledge-bases/:id/faq/entries` |
| `POST /v1/knowledge-bases/:id/faq/entry` | `POST /api/v1/knowledge-bases/:id/faq/entry` |
| `POST /v1/knowledge-bases/:id/faq/entries` | `POST /api/v1/knowledge-bases/:id/faq/entries` |
| `GET /v1/knowledge-bases/:id/faq/entries/:entry_id` | 同名 |
| `PATCH /v1/knowledge-bases/:id/faq/entries/:entry_id` | `PUT` 同名 |
| `DELETE /v1/knowledge-bases/:id/faq/entries` | 同名 |
| `POST /v1/knowledge-bases/:id/faq/search` | 同名 |
| `GET /v1/knowledge-bases/:id/faq/entries/export` | 同名 |

---

## 7. 直接知识检索（不经 LLM）

| 包装层 | 上游 |
|---|---|
| `POST /v1/knowledge-bases/:id/hybrid-search` | `GET /api/v1/knowledge-bases/:id/hybrid-search` （注意上游用 GET+body，包装层建议改 POST） |
| `POST /v1/search` | `POST /api/v1/knowledge-search` | 跨库检索 |

---

## 8. 会话管理

| 包装层 | 上游 |
|---|---|
| `POST /v1/sessions` | `POST /api/v1/sessions` |
| `GET /v1/sessions` | `GET /api/v1/sessions` |
| `GET /v1/sessions/:id` | `GET /api/v1/sessions/:id` |
| `PATCH /v1/sessions/:id` | `PUT /api/v1/sessions/:id` |
| `DELETE /v1/sessions/:id` | `DELETE /api/v1/sessions/:id` |
| `DELETE /v1/sessions/batch` | `DELETE /api/v1/sessions/batch` |
| `DELETE /v1/sessions/:id/messages` | `DELETE /api/v1/sessions/:id/messages` |
| `POST /v1/sessions/:sid/generate-title` | `POST /api/v1/sessions/:session_id/generate_title` |
| `POST /v1/sessions/:sid/pin` | `POST /api/v1/sessions/:session_id/pin` |
| `DELETE /v1/sessions/:id/pin` | `DELETE /api/v1/sessions/:id/pin` |

---

## 9. 对话 / 检索增强生成（SSE 流式）

| 包装层 | 上游 | 备注 |
|---|---|---|
| 🌊 `POST /v1/chat/rag/:sid` | `POST /api/v1/knowledge-chat/:session_id` | RAG 模式 |
| 🌊 `POST /v1/chat/agent/:sid` | `POST /api/v1/agent-chat/:session_id` | Agent / 工具调用 |
| `POST /v1/sessions/:sid/stop` | `POST /api/v1/sessions/:session_id/stop` | 中断 |
| 🌊 `GET /v1/chat/:sid/stream` | `GET /api/v1/sessions/continue-stream/:session_id` | 断线续传 |

### 9.1 SSE 反代要点
1. Response header `Content-Type: text/event-stream`、`Cache-Control: no-cache`、`X-Accel-Buffering: no`
2. `httputil.ReverseProxy{FlushInterval: -1}` — 每写即刷
3. 客户端 ctx 取消 → 显式 cancel 上游 request
4. 不要解析 body；不要 buffer；不要做 keep-alive 池长连接（直连）
5. SSE 心跳由 WeKnora 上游产生，包装层透传即可

---

## 10. 消息历史

| 包装层 | 上游 |
|---|---|
| `GET /v1/messages/:sid/load` | `GET /api/v1/messages/:session_id/load` |
| `DELETE /v1/messages/:sid/:id` | `DELETE /api/v1/messages/:session_id/:id` |
| `POST /v1/messages/search` | `POST /api/v1/messages/search` |
| `GET /v1/messages/chat-history-stats` | `GET /api/v1/messages/chat-history-stats` |

---

## 11. 文件代理

| 包装层 | 内部行为 |
|---|---|
| 🔗 `GET /v1/files/:wrapper_file_id?sig=...&expires=...` | 校验包装层签名 → 查 `wrapper_files` 表得到 `weknora_file_path` 与 `weknora_tenant_id` → 用对应 X-API-Key 调 WeKnora `/files?file_path=...` 取流式回传 |

**不向外暴露** WeKnora `/api/v1/files/presigned`（HMAC 包含 WeKnora 内部租户 ID 与路径）。

---

## 12. 模型 / 检索 / 系统配置（按需开放，默认仅管理员）

仅在调用方需要自行管理 WeKnora 内部资源时按需打开。**默认包装层不暴露**：

- `/api/v1/models/*`（模型管理）
- `/api/v1/initialization/*`（初始化、Ollama 等）
- `/api/v1/system/*`
- `/api/v1/web-search*`、`/api/v1/vector-stores*`、`/api/v1/mcp-services*`
- `/api/v1/organizations/*`、`/api/v1/knowledge-bases/:id/shares`（组织 / 共享空间）
- `/api/v1/agents/*`、`/api/v1/skills/*`
- `/api/v1/datasource/*`
- `/api/v1/knowledgebase/:kb_id/wiki/*`
- `/api/v1/im/*`、`/api/v1/im-channels/*`、`/api/v1/wechat/*`

若需要，放在包装层 `/v1/admin/*` 路径下并要求最高角色。

---

## 13. 包装层独有端点

| 端点 | 用途 |
|---|---|
| `POST /v1/auth/token` | 包装层颁发 JWT（用户名密码 / OAuth Code 等） |
| `POST /v1/auth/refresh` | 刷新包装层 JWT |
| `GET /v1/health` | 健康检查（同时探活 WeKnora） |
| `GET /v1/version` | 包装层版本 + 检测到的 WeKnora 版本 |
| 🔒 `GET /v1/admin/audit-events` | 审计日志查询 |
| 🔒 `POST /v1/admin/tenants/:id/freeze` | 冻结租户（拒绝转发，但保留数据） |

---

## 14. 待澄清

- WeKnora `evaluation`、`agents/:id/tool-approvals` 等管理后台型端点是否需要面向调用方暴露 → 默认**不**暴露
- WeKnora `initialization/*` 类一次性引导端点是否由包装层管理或部署脚本管理 → 建议放运维流程，**不暴露**
- 跨包装层租户的数据迁移 / 备份接口 → 不在 V1 范围
