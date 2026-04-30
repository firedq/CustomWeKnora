# WeKnora API 参考文档

## 概述

WeKnora 提供完整的 RESTful API，覆盖知识库管理、文档上传、Wiki 与知识图谱、直接知识检索以及对话聊天等功能。

- **Base URL**: `http(s)://<host>/api/v1`
- **数据格式**: JSON（Content-Type: application/json）
- **响应结构**: `{"success": true, "data": ...}` / `{"success": false, "message": "..."}`

---

## 认证

所有接口（除 `/health` 和认证相关接口外）均需认证，支持两种方式：

| 方式 | Header |
|------|--------|
| JWT Bearer Token | `Authorization: Bearer <token>` |
| API Key | `X-API-Key: <api_key>` |

### 获取 Token

```http
POST /api/v1/auth/login
Content-Type: application/json

{
  "username": "admin",
  "password": "your_password"
}
```

**响应**:
```json
{
  "success": true,
  "data": {
    "token": "eyJ...",
    "refresh_token": "eyJ..."
  }
}
```

---

## 1. 知识库管理

### 创建知识库

```http
POST /api/v1/knowledge-bases
```

**请求体**:
```json
{
  "name": "我的知识库",
  "type": "document",
  "description": "描述",
  "embedding_model_id": "model-id",
  "extract_config": {
    "enabled": true,
    "text": "关系描述",
    "tags": ["标签1"],
    "nodes": [{"name": "人物"}, {"name": "组织"}],
    "relations": [{"node1": "人物", "node2": "组织", "type": "属于"}]
  }
}
```

**知识库类型** (`type`):
- `document` — 文档型（上传文件/URL/手动输入）
- `faq` — 问答型（Q&A 条目）
- `wiki` — Wiki 型（结构化 Wiki 页面，支持知识图谱）

**知识图谱** (`extract_config`): 仅当 `enabled: true` 时生效，需同时定义节点类型、关系类型和提取说明。

**响应**: `201 Created`，返回创建的知识库对象。

---

### 列出知识库

```http
GET /api/v1/knowledge-bases
```

---

### 获取知识库详情

```http
GET /api/v1/knowledge-bases/:id
```

---

### 更新知识库

```http
PUT /api/v1/knowledge-bases/:id
Content-Type: application/json

{
  "name": "新名称",
  "description": "新描述",
  "config": { ... }
}
```

---

### 删除知识库

```http
DELETE /api/v1/knowledge-bases/:id
```

---

### 复制知识库（异步）

```http
POST /api/v1/knowledge-bases/copy
Content-Type: application/json

{
  "source_id": "kb-id-source",
  "target_id": "kb-id-target"
}
```

**查询进度**:
```http
GET /api/v1/knowledge-bases/copy/progress/:task_id
```

---

### 混合搜索（向量 + 关键词）

```http
GET /api/v1/knowledge-bases/:id/hybrid-search
Content-Type: application/json

{
  "query_text": "搜索内容",
  "top_k": 10
}
```

---

## 2. 文档上传与知识管理

### 从文件上传创建知识

```http
POST /api/v1/knowledge-bases/:id/knowledge/file
Content-Type: multipart/form-data

file=<二进制文件>
fileName=custom_name.pdf      (可选，自定义文件名)
metadata={"key":"value"}      (可选，JSON元数据)
enable_multimodel=true        (可选，启用多模态解析)
tag_id=<tag-id>               (可选，分类标签)
```

支持格式：PDF、Word、Excel、PPT、图片、Markdown、TXT、CSV、代码文件等。

**响应**: `200 OK`，返回创建的知识对象。

---

### 从 URL 创建知识

```http
POST /api/v1/knowledge-bases/:id/knowledge/url
Content-Type: application/json

{
  "url": "https://example.com/doc.pdf",
  "file_name": "custom.pdf",
  "file_type": "pdf",
  "enable_multimodel": false,
  "title": "文档标题",
  "tag_id": "tag-id"
}
```

支持网页抓取和远程文件下载。当 URL 路径含已知扩展名（如 `.pdf`）时自动切换为文件下载模式。

---

### 手动创建 Markdown 知识

```http
POST /api/v1/knowledge-bases/:id/knowledge/manual
Content-Type: application/json

{
  "title": "文档标题",
  "content": "# 内容\n这是 Markdown 正文",
  "tag_id": "tag-id"
}
```

---

### 列出知识库下的知识条目

```http
GET /api/v1/knowledge-bases/:id/knowledge?page=1&page_size=20&keyword=关键词&tag_id=xxx&file_type=pdf
```

---

### 获取知识详情

```http
GET /api/v1/knowledge/:id
```

---

### 批量获取知识

```http
GET /api/v1/knowledge/batch?ids=id1&ids=id2&kb_id=xxx
```

---

### 更新知识条目

```http
PUT /api/v1/knowledge/:id
Content-Type: application/json

{ "title": "新标题", ... }
```

---

### 更新手工 Markdown 知识

```http
PUT /api/v1/knowledge/manual/:id
Content-Type: application/json

{
  "title": "新标题",
  "content": "更新后的内容"
}
```

---

### 重新解析知识（异步）

```http
POST /api/v1/knowledge/:id/reparse
```

---

### 下载原始文件

```http
GET /api/v1/knowledge/:id/download
```

---

### 预览文件（浏览器内嵌）

```http
GET /api/v1/knowledge/:id/preview
```

---

### 删除知识

```http
DELETE /api/v1/knowledge/:id
```

---

### 批量删除知识

```http
POST /api/v1/knowledge/batch-delete
Content-Type: application/json

{
  "kb_id": "knowledge-base-id",
  "ids": ["id1", "id2"]
}
```

单次最多 200 条，异步执行。

---

### 清空知识库内容（异步）

```http
DELETE /api/v1/knowledge-bases/:id/knowledge
```

---

### 移动知识到其他知识库（异步）

```http
POST /api/v1/knowledge/move
Content-Type: application/json

{
  "knowledge_ids": ["id1", "id2"],
  "source_kb_id": "source-id",
  "target_kb_id": "target-id",
  "mode": "reuse_vectors"
}
```

`mode` 可选：`reuse_vectors`（复用向量）/ `reparse`（重新解析）。

**查询进度**:
```http
GET /api/v1/knowledge/move/progress/:task_id
```

---

### 搜索知识文件列表

```http
GET /api/v1/knowledge/search?keyword=关键词&offset=0&limit=20&file_types=pdf,docx
```

---

### 批量更新知识标签

```http
PUT /api/v1/knowledge/tags
Content-Type: application/json

{
  "kb_id": "kb-id",
  "updates": {
    "knowledge-id-1": "new-tag-id",
    "knowledge-id-2": null
  }
}
```

---

## 3. 分块（Chunk）管理

### 列出分块

```http
GET /api/v1/chunks/:knowledge_id
```

### 获取单个分块

```http
GET /api/v1/chunks/by-id/:id
```

### 更新分块

```http
PUT /api/v1/chunks/:knowledge_id/:id
Content-Type: application/json

{ "content": "新内容" }
```

### 删除分块

```http
DELETE /api/v1/chunks/:knowledge_id/:id
```

### 删除知识下所有分块

```http
DELETE /api/v1/chunks/:knowledge_id
```

---

## 4. 标签管理

```http
GET    /api/v1/knowledge-bases/:id/tags           # 列出标签
POST   /api/v1/knowledge-bases/:id/tags           # 创建标签  {"name": "标签名"}
PUT    /api/v1/knowledge-bases/:id/tags/:tag_id   # 更新标签
DELETE /api/v1/knowledge-bases/:id/tags/:tag_id   # 删除标签
```

---

## 5. Wiki 与知识图谱

> Wiki 功能需要知识库类型为 `wiki` 或在 `WikiConfig` 中启用 Wiki 特性。

### Wiki 页面 CRUD

```http
GET    /api/v1/knowledgebase/:kb_id/wiki/pages                     # 列出页面
POST   /api/v1/knowledgebase/:kb_id/wiki/pages                     # 创建页面
GET    /api/v1/knowledgebase/:kb_id/wiki/pages/*slug               # 获取页面
PUT    /api/v1/knowledgebase/:kb_id/wiki/pages/*slug               # 更新页面
DELETE /api/v1/knowledgebase/:kb_id/wiki/pages/*slug               # 删除页面
```

**创建/更新页面请求体**:
```json
{
  "title": "页面标题",
  "slug": "page-slug",
  "content": "# Markdown 内容\n[[其他页面]]",
  "page_type": "general",
  "status": "published"
}
```

支持 `[[WikiLink]]` 双向链接语法，系统自动维护页面间引用关系。

---

### 特殊页面

```http
GET /api/v1/knowledgebase/:kb_id/wiki/index   # 首页（不存在时自动创建）
GET /api/v1/knowledgebase/:kb_id/wiki/log     # 操作日志页
```

---

### 知识图谱（链接图）

```http
GET /api/v1/knowledgebase/:kb_id/wiki/graph
```

**响应**（可用于可视化展示）：
```json
{
  "nodes": [{"id": "slug", "title": "页面标题", ...}],
  "edges": [{"source": "slug1", "target": "slug2", ...}]
}
```

---

### Wiki 统计

```http
GET /api/v1/knowledgebase/:kb_id/wiki/stats
```

返回页面数、链接数、孤立页面数等聚合统计。

---

### Wiki 全文搜索

```http
GET /api/v1/knowledgebase/:kb_id/wiki/search?q=关键词&limit=10
```

---

### Wiki 维护操作

```http
POST /api/v1/knowledgebase/:kb_id/wiki/rebuild-links   # 重建所有链接引用
GET  /api/v1/knowledgebase/:kb_id/wiki/lint            # 健康检查（断链、孤立页面等）
POST /api/v1/knowledgebase/:kb_id/wiki/auto-fix        # 自动修复可修复的问题
```

---

### Wiki 问题管理

```http
GET /api/v1/knowledgebase/:kb_id/wiki/issues?slug=xxx&status=pending
PUT /api/v1/knowledgebase/:kb_id/wiki/issues/:issue_id/status
    Body: {"status": "ignored"}   # pending / ignored / resolved
```

---

## 6. FAQ 管理

### 条目 CRUD

```http
GET    /api/v1/knowledge-bases/:id/faq/entries              # 列出条目
POST   /api/v1/knowledge-bases/:id/faq/entry               # 创建单条
POST   /api/v1/knowledge-bases/:id/faq/entries             # 批量导入（upsert）
GET    /api/v1/knowledge-bases/:id/faq/entries/:entry_id   # 获取单条
PUT    /api/v1/knowledge-bases/:id/faq/entries/:entry_id   # 更新
DELETE /api/v1/knowledge-bases/:id/faq/entries             # 批量删除
```

### 搜索 FAQ

```http
POST /api/v1/knowledge-bases/:id/faq/search
Content-Type: application/json

{"query": "搜索内容", "top_k": 5}
```

### 导出 FAQ

```http
GET /api/v1/knowledge-bases/:id/faq/entries/export
```

### 添加相似问题

```http
POST /api/v1/knowledge-bases/:id/faq/entries/:entry_id/similar-questions
Content-Type: application/json

{"questions": ["相似问法1", "相似问法2"]}
```

---

## 7. 直接知识检索（不经 LLM）

直接对知识库执行语义检索，返回原始分块结果，不经大模型总结。

### 知识库混合检索

```http
GET /api/v1/knowledge-bases/:id/hybrid-search
Content-Type: application/json

{
  "query_text": "检索内容",
  "top_k": 10
}
```

同时使用向量相似度检索和关键词检索，结果自动合并排序。

---

### 跨知识库语义搜索

```http
POST /api/v1/knowledge-search
Content-Type: application/json

{
  "query": "检索内容",
  "knowledge_base_ids": ["kb-id-1", "kb-id-2"],
  "knowledge_ids": ["file-id-1"]
}
```

至少提供一个 `knowledge_base_ids` 或 `knowledge_ids`。返回相关分块列表，不调用 LLM。

---

## 8. 聊天功能

### 8.1 会话管理

```http
POST   /api/v1/sessions              # 创建会话
GET    /api/v1/sessions              # 列出会话（支持分页）
GET    /api/v1/sessions/:id          # 获取会话详情
PUT    /api/v1/sessions/:id          # 更新会话
DELETE /api/v1/sessions/:id          # 删除会话
DELETE /api/v1/sessions/batch        # 批量删除（或 delete_all=true 清空）
DELETE /api/v1/sessions/:id/messages # 清空会话消息
```

**创建会话**:
```json
{
  "title": "会话标题（可选）",
  "description": "描述（可选）"
}
```

---

### 8.2 知识问答（RAG 模式，SSE 流式）

```http
POST /api/v1/knowledge-chat/:session_id
Content-Type: application/json

{
  "query": "用户问题",
  "knowledge_base_ids": ["kb-id-1"],
  "knowledge_ids": ["file-id-1"],
  "agent_id": "agent-id",
  "web_search_enabled": false,
  "summary_model_id": "model-id",
  "mentioned_items": [
    {"id": "kb-id", "type": "kb"},
    {"id": "file-id", "type": "file"}
  ],
  "images": [{"data": "data:image/png;base64,..."}],
  "attachment_uploads": [
    {"data": "<base64>", "file_name": "doc.pdf", "file_size": 102400}
  ],
  "enable_memory": false,
  "disable_title": false,
  "channel": "api"
}
```

**响应**: SSE 事件流（`text/event-stream`），逐步推送 LLM 生成内容和检索过程事件。

关键 SSE 事件类型：
- `agent_query` — 请求开始
- `agent_thinking` — LLM 思考中
- `agent_final_answer` — 最终回答（流式）
- `agent_tool_call` / `agent_tool_result` — 工具调用（检索、图片分析等）
- `agent_complete` — 完成
- `error` — 错误信息

---

### 8.3 Agent 问答（工具调用模式，SSE 流式）

```http
POST /api/v1/agent-chat/:session_id
Content-Type: application/json

{
  "query": "用户问题",
  "agent_id": "agent-id",
  "agent_enabled": true,
  ... （同上其他字段）
}
```

当 Agent 配置了工具（Web 搜索、代码执行等）时，支持多轮工具调用推理。响应格式同 SSE 流。

---

### 8.4 停止流式响应

```http
POST /api/v1/sessions/:session_id/stop
Content-Type: application/json

{"message_id": "assistant-message-id"}
```

---

### 8.5 继续接收活跃流

```http
GET /api/v1/sessions/continue-stream/:session_id
```

断线重连时使用，恢复未完成的 SSE 流。

---

### 8.6 消息管理

```http
GET    /api/v1/messages/:session_id/load          # 加载历史消息（向上滚动）
DELETE /api/v1/messages/:session_id/:id            # 删除单条消息
POST   /api/v1/messages/search                     # 历史对话搜索
```

**历史搜索**:
```json
{
  "query": "搜索内容",
  "session_id": "可选，限定会话"
}
```

---

### 8.7 自动生成会话标题

```http
POST /api/v1/sessions/:session_id/generate_title
Content-Type: application/json

{"messages": [...]}
```

---

## 9. 通用说明

### 错误响应格式

```json
{
  "success": false,
  "message": "错误描述",
  "code": "error_code"
}
```

常见 HTTP 状态码：`400`（参数错误）、`401`（未认证）、`403`（权限不足）、`404`（资源不存在）、`409`（重复资源）、`500`（服务器错误）。

### 分页参数

大部分列表接口支持：`page`（页码，从 1 开始）、`page_size`（每页数量）。

### 异步任务

复制、移动、清空等耗时操作均为异步，接口立即返回 `task_id`，通过进度接口轮询状态：`pending → processing → completed / failed`。

### Swagger 文档

非生产环境下，可通过 `/swagger/index.html` 访问交互式 API 文档。
