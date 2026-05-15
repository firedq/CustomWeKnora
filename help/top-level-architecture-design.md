# WeKnora 顶层架构设计文档

## 1. 文档目标

本文档从工程实现角度描述 WeKnora 的顶层架构，覆盖系统边界、主要组件、核心数据流、部署形态与扩展点。文档基于当前仓库代码与配置整理，主要参考：

- `cmd/server/main.go`：Go App 服务启动入口
- `internal/container/container.go`：依赖注入与组件装配
- `internal/router/router.go`：HTTP API 与中间件路由
- `internal/application/service/*`：核心业务服务
- `internal/application/service/chat_pipeline/*`：RAG 对话流水线
- `internal/agent/*`：ReAct Agent 引擎、工具与 Skills
- `docreader/main.py`：Python DocReader gRPC 服务
- `docker-compose.yml`、`helm/`：容器化与 Kubernetes 部署视图

## 2. 系统定位

WeKnora 是一个面向企业知识管理、RAG 问答、Agent 推理与自动 Wiki 构建的知识框架。系统将文档、FAQ、外部数据源、IM 消息与网页内容统一沉淀为知识资产，并通过混合检索、知识图谱、LLM、MCP 工具和 Agent Skills 提供问答与推理能力。

核心能力包括：

- 知识库管理：文档型、FAQ 型、Wiki 型知识库。
- 知识入库：文件、URL、手工 Markdown、外部数据源同步。
- 文档解析：独立 DocReader 服务将文件或 URL 转换为 Markdown 与图片引用。
- 检索增强：向量检索、关键词检索、FAQ 策略、Graph RAG、Wiki Boost。
- 对话问答：普通 RAG 流水线与 ReAct Agent 模式。
- Agent 扩展：内置工具、MCP 工具、Web Search、DuckDB 数据分析、Skills 沙盒执行。
- 多端接入：Web UI、Lite/桌面端、微信小程序、IM 机器人、REST API、MCP Server。
- 私有化部署：Docker Compose、Helm、桌面 Lite、可替换模型/存储/向量库。

## 3. 顶层架构线框图

```text
+--------------------------------------------------------------------------------------+
|                                      接入层                                          |
+--------------------------------------------------------------------------------------+
| Web UI(Vue3/TDesign) | Lite/Desktop(Wails) | 微信小程序 | Go Client/REST API          |
| IM 机器人(企微/飞书/Slack/Telegram/钉钉/Mattermost/微信) | MCP Server 对外工具入口     |
+----------------------------------------------+---------------------------------------+
                                               |
                                               v
+--------------------------------------------------------------------------------------+
|                                  WeKnora Go App                                      |
+--------------------------------------------------------------------------------------+
| Gin Router                                                                           |
| - CORS / RequestID / Language / Logger / Recovery / ErrorHandler                     |
| - Auth(JWT Bearer / API Key) / File Proxy / SSE / Swagger / IM Callback              |
+----------------------------------------------+---------------------------------------+
                                               |
                                               v
+--------------------------------------------------------------------------------------+
| Handler 层                                                                           |
| Auth | Tenant/User | KnowledgeBase | Knowledge/FAQ/Chunk | Session/Message/Chat      |
| Model | Agent/Skill | MCP/WebSearch/VectorStore | DataSource | Wiki | IM | System    |
+----------------------------------------------+---------------------------------------+
                                               |
                                               v
+--------------------------------------------------------------------------------------+
| Application Service 层                                                               |
| Tenant/User/Organization | KB/Knowledge/Chunk/Tag | Session/Message | Model          |
| Agent/Wiki/Memory | DataSource Scheduler | Evaluation | FileService | WeKnoraCloud  |
+--------------------------+-------------------+--------------------+----------------+
                           |                   |                    |
                           v                   v                    v
+--------------------------+     +-----------------------------+    +------------------+
| Chat Pipeline            |     | ReAct Agent Engine          |    | Task Runtime     |
| History                  |     | Tool Registry               |    | asynq + Redis    |
| Query Understand         |     | Knowledge/Web/MCP/Skills    |    | 或 Lite 同步执行 |
| Search/Rerank/Merge      |     | DuckDB/Wiki/Graph/Memory    |    |                  |
| LLM Stream Completion    |     | EventBus Streaming          |    |                  |
+------------+-------------+     +--------------+--------------+    +---------+--------+
             |                                  |                         |
             +------------------+---------------+-------------------------+
                                |
                                v
+--------------------------------------------------------------------------------------+
| Repository / Registry 层                                                             |
| GORM Repositories | RetrieveEngineRegistry | VectorStoreRegistry | Graph Repository   |
+----------------+------------------+---------------------+--------------------------+
                 |                  |                     |
                 v                  v                     v
+-------------------------+  +---------------------------+  +-------------------------+
| 业务数据库              |  | 检索索引                  |  | 图数据库                |
| PostgreSQL / SQLite     |  | Postgres/ES/Qdrant        |  | Neo4j                   |
| Tenant/User/KB/Chunk    |  | Milvus/Weaviate/SQLiteVec |  | Graph RAG / Memory      |
+-------------------------+  +---------------------------+  +-------------------------+

+--------------------------------------------------------------------------------------+
|                              可插拔基础设施与外部服务                                |
+--------------------------------------------------------------------------------------+
| DocReader gRPC(Python 文档解析) | 对象存储(Local/MinIO/COS/TOS/S3/OSS)                |
| 模型 Provider(Chat/Embedding/Rerank/VLM/ASR) | Web Search Provider                    |
| Redis(任务队列/上下文/进度/分布式状态) | OpenTelemetry/Jaeger/Langfuse             |
| 外部 MCP Services(SSE/HTTP Streamable) | Skills Sandbox(Docker/本地执行策略)       |
+--------------------------------------------------------------------------------------+

+--------------------------------------------------------------------------------------+
|                                      知识来源                                        |
+--------------------------------------------------------------------------------------+
| 文件/图片/表格/PDF/PPT/Markdown | 网页 URL/远程文件 | 数据源同步(飞书/Notion/语雀) |
+--------------------------------------------------------------------------------------+
```

## 4. 代码分层与职责

| 层级 | 主要目录 | 职责 |
|------|----------|------|
| 接入层 | `frontend/`、`miniprogram/`、`client/`、`mcp-server/`、`internal/im/` | 提供 Web、桌面、小程序、SDK、MCP、IM 等入口 |
| HTTP 层 | `cmd/server/`、`internal/router/`、`internal/middleware/`、`internal/handler/` | 启动服务、装配路由、中间件、认证、SSE、参数解析与响应 |
| 应用服务层 | `internal/application/service/` | 编排知识库、知识、会话、消息、Agent、Wiki、数据源、模型、共享空间等业务 |
| 对话流水线 | `internal/application/service/chat_pipeline/` | 以插件事件链组织 RAG 检索、重排、合并、上下文构造和流式生成 |
| Agent 层 | `internal/agent/` | ReAct 循环、工具注册、工具调用、Skills、MCP、数据分析、Wiki 工具 |
| 仓储层 | `internal/application/repository/` | GORM 业务数据访问、向量/关键词/图谱检索仓储适配 |
| 基础设施层 | `internal/models/`、`internal/infrastructure/`、`internal/mcp/`、`internal/sandbox/`、`docreader/` | 模型 Provider、文档解析、网页搜索、MCP Client、沙盒、存储与外部服务适配 |
| 部署运维 | `docker-compose.yml`、`helm/`、`scripts/`、`migrations/` | 本地、容器、Kubernetes、迁移和脚本化运维 |

## 5. 运行入口与依赖装配

### 5.1 Go App 启动链路

`cmd/server/main.go` 是服务端入口：

1. 根据 `GIN_MODE` 设置 Gin 运行模式。
2. 调用 `container.BuildContainer(runtime.GetContainer())` 构建 `dig` 依赖注入容器。
3. 从容器中解析 `config.Config`、`gin.Engine`、`tracing.Tracer` 和资源清理器。
4. 通过 `listenWithRetry` 监听 `config.yaml` 中的 `server.host` 与 `server.port`。
5. 处理系统信号，关闭 HTTP Server 并执行资源清理。

`internal/container/container.go` 是整个后端的装配中心，负责注册：

- 配置、日志、OpenTelemetry、Langfuse。
- 数据库、Redis、文件服务、DocReader、Ollama、Neo4j、DuckDB。
- 检索引擎注册表与 Vector Store 注册能力。
- Repository、Service、Handler。
- Chat Pipeline 插件。
- asynq 异步任务或 Lite 模式同步任务。
- 数据源同步调度器。
- IM Adapter 工厂和 MCP Manager。

### 5.2 路由与认证

`internal/router/router.go` 使用 Gin 注册系统路由：

- `/health`：健康检查，无认证。
- `/swagger/*any`：非 release 模式 API 文档。
- `/api/v1/*`：主要 REST API。
- 文件代理与预签名文件访问。
- IM 回调路由在认证中间件前注册，由各平台签名校验。

认证由 `middleware.Auth` 统一处理，支持 JWT Bearer Token 与 API Key。业务处理器覆盖知识库、知识、FAQ、Chunk、Session、Message、Model、MCP、Web Search、Vector Store、Agent、Skill、Organization、DataSource、Wiki、IM 等模块。

## 6. 核心业务域

### 6.1 租户、用户与共享空间

租户是主要隔离边界。请求上下文中携带租户与用户信息，服务层通过 `types.TenantIDContextKey`、`types.TenantInfoContextKey` 等上下文值控制数据范围。共享空间相关能力由 `OrganizationService`、`KBShareService`、`AgentShareService` 实现，用于跨成员共享知识库和 Agent。

### 6.2 知识库与知识条目

知识库由 `KnowledgeBaseService` 管理，支持不同知识库类型和索引策略。知识条目由 `KnowledgeService` 管理，来源包括：

- 文件上传。
- URL 导入。
- 手工 Markdown。
- FAQ 导入。
- 数据源同步。
- 对话历史附件或消息派生内容。

知识库级配置会影响后续处理：

- Embedding 模型、Rerank 模型、Summary 模型。
- 文档分块策略。
- 存储 Provider。
- 向量索引、关键词索引、Wiki、Graph RAG 开关。
- FAQ 检索和问答匹配策略。

### 6.3 模型管理

`internal/models/` 将模型按能力拆分：

- `chat/`：聊天模型。
- `embedding/`：向量模型。
- `rerank/`：重排模型。
- `vlm/`：视觉语言模型。
- `asr/`：语音识别模型。
- `provider/`：OpenAI、Azure OpenAI、DeepSeek、Qwen、Gemini、Ollama、WeKnora Cloud 等模型厂商适配。

业务服务通过 `ModelService` 获取具体模型实例，流水线和 Agent 不直接绑定某个厂商。

## 7. 知识入库链路

```text
+-------------+      +------------------+      +------------------+
| Client/UI   | ---> | KnowledgeHandler | ---> | KnowledgeService |
| IM/API      |      | 参数解析/鉴权上下文 |      | 创建知识元数据     |
+-------------+      +------------------+      +---------+--------+
                                                           |
                                                           v
                     +------------------+      +------------------+
                     | FileService      | <--- | 保存原始文件/图片 |
                     | Local/OSS/S3/... |      +------------------+
                     +------------------+
                                                           |
                                                           v
                     +------------------+      +------------------+
                     | TaskEnqueuer     | <--- | 文档/FAQ/Wiki    |
                     | asynq 或同步执行 |      | 后处理任务入队    |
                     +--------+---------+      +------------------+
                              |
                              v
                     +------------------+      +------------------+
                     | ProcessDocument  | ---> | DocReader gRPC   |
                     | ProcessFAQImport |      | 文件/URL -> MD   |
                     +--------+---------+      +--------+---------+
                              |                         |
                              |                         v
                              |              +---------------------+
                              |              | Markdown/图片/元数据 |
                              |              +----------+----------+
                              v                         |
                     +------------------+ <-------------+
                     | 清洗/分块/父子块  |
                     | 图片处理/摘要生成 |
                     +--------+---------+
                              |
                +-------------+--------------+
                |                            |
                v                            v
       +------------------+        +---------------------------+
       | DB               |        | Embedding/VLM/LLM         |
       | Knowledge/Chunk  |        | 向量/摘要/图片描述/图谱抽取 |
       +--------+---------+        +-------------+-------------+
                |                                |
                v                                v
       +------------------+        +---------------------------+
       | Retriever Store  |        | Neo4j / Wiki              |
       | 向量/关键词索引   |        | 图谱提取/Wiki 页面生成     |
       +------------------+        +---------------------------+
```

入库关键点：

- DocReader 仅负责解析，图片最终由 Go App 写入配置的文件服务。
- Redis 存在时使用 asynq 异步任务；Lite 模式下可退化为同步任务执行器。
- Chunk 写入后按知识库索引策略写入检索后端。
- Graph RAG 依赖 Neo4j 与知识库图谱配置。
- Wiki 模式通过 `WikiIngestService` 将文档内容增量整理为结构化 Markdown Wiki 页面。

## 8. 普通 RAG 对话链路

普通问答由 `SessionHandler` 处理 SSE，请求进入 `SessionService.KnowledgeQA` 后动态组装 Pipeline。

```text
+----------+
| 用户问题 |
+----+-----+
     |
     v
+-------------------------------+
| 解析知识库/文件/模型/租户范围 |
+----+--------------------------+
     |
     v
+-------------------------------+
| 构建 ChatManage               |
| Query / Session / SearchTargets|
| Retrieval / Prompt / EventBus |
+----+--------------------------+
     |
     v
+-------------------------------+
| LOAD_HISTORY                  |
+----+--------------------------+
     |
     v
+-------------------------------+
| QUERY_UNDERSTAND              |
| 改写 / 扩展 / 意图识别 / 实体抽取 |
+----+--------------------------+
     |
     v
+-------------------------------+
| CHUNK_SEARCH_PARALLEL         |
| 向量 / 关键词 / 图谱 / Wiki / 多 KB |
+----+--------------------------+
     |
     v
+-------------------------------+
| CHUNK_RERANK                  |
+----+--------------------------+
     |
     v
+-------------------------------+
| WEB_FETCH                     |
| 可选网页内容抓取              |
+----+--------------------------+
     |
     v
+-------------------------------+
| CHUNK_MERGE + FILTER_TOP_K    |
+----+--------------------------+
     |
     v
+-------------------------------+
| DATA_ANALYSIS                 |
| 可选表格/附件分析             |
+----+--------------------------+
     |
     v
+-------------------------------+
| INTO_CHAT_MESSAGE             |
| 构造 LLM 上下文               |
+----+--------------------------+
     |
     v
+-------------------------------+
| CHAT_COMPLETION_STREAM        |
+----+--------------------------+
     |
     v
+-------------------------------+
| EventBus + StreamManager      |
| SSE 流式返回 / 断线续传       |
+-------------------------------+
```

Pipeline 插件按事件注册到 `chatpipeline.EventManager`。普通 RAG 模式的典型阶段包括：

- 加载历史上下文。
- 查询理解、实体提取、查询改写与扩展。
- 并行检索向量、关键词、图谱和 Wiki 增强结果。
- Rerank 与结果合并。
- 附件/表格数据分析。
- 构造 Prompt 与流式调用 Chat 模型。
- 通过 EventBus/SSE 输出引用、答案分片和完成事件。

## 9. Agent 推理链路

Agent 模式由 `SessionService.AgentQA` 构建运行时 Agent 配置，再由 `AgentService.CreateAgentEngine` 创建 ReAct 引擎。

```text
+----------------+
| AgentQA 请求   |
+-------+--------+
        |
        v
+-------------------------------+
| 合并运行时配置                |
| CustomAgent + Tenant + Request|
+-------+-----------------------+
        |
        +------------------------+-------------------------+
        |                        |                         |
        v                        v                         v
+----------------+       +----------------+        +----------------+
| 加载模型       |       | 加载上下文     |        | 注册工具       |
| Chat/Rerank/VLM|       | Redis/内存     |        | ToolRegistry   |
+-------+--------+       +-------+--------+        +-------+--------+
        |                        |                         |
        |                        |                         v
        |                        |      +----------------------------------------+
        |                        |      | ToolRegistry                           |
        |                        |      | - knowledge_search                     |
        |                        |      | - web_search / web_fetch               |
        |                        |      | - data_analysis / data_schema(DuckDB)  |
        |                        |      | - query_knowledge_graph                |
        |                        |      | - wiki_read/write/rename/delete/issue  |
        |                        |      | - MCP Tools                            |
        |                        |      | - skill_read / skill_execute           |
        |                        |      | - final_answer                         |
        |                        |      +-------------------+--------------------+
        |                        |                          |
        +------------------------+--------------------------+
                                 |
                                 v
                      +----------------------+
                      | ReAct Agent Engine   |
                      +----------+-----------+
                                 |
                                 v
                      +----------------------+
                      | Think -> Act -> Observe |
                      | 多轮推理与工具调用       |
                      +----------+-----------+
                                 |
                 +---------------+----------------+
                 |                                |
                 v                                v
       +----------------------+        +----------------------+
       | EventBus             |        | Context / Memory     |
       | thought/tool/final   |        | 压缩与记忆           |
       +----------+-----------+        +----------------------+
                  |
                  v
       +----------------------+
       | SSE 流式响应         |
       +----------------------+
```

Agent 的扩展点集中在工具层：

- 内置知识检索工具访问知识库与 Chunk。
- Web Search 和 Web Fetch 访问外部网页。
- DuckDB 支持 CSV/Excel 等数据分析。
- MCP Manager 连接租户启用的 MCP 服务。
- Skills Manager 加载本地技能目录，并通过沙盒执行。
- Wiki 工具支持 Agent 修改和维护 Wiki 页面。

## 10. Wiki 与知识图谱

### 10.1 Wiki 模式

Wiki 模式由 `WikiIngestService` 负责，核心思想是将上传文档或数据源内容增量转化为互相关联的 Markdown 页面。处理流程包含：

- 收集待处理知识条目。
- 从 Chunk 重建文档内容。
- 使用 LLM 生成或更新摘要页、实体页、概念页、综合页、对比页等。
- 去重、合并、重命名和索引页重建。
- 记录来源 Chunk，便于回溯引用。

### 10.2 Graph RAG

Graph RAG 使用 Neo4j 存储实体和关系。文档处理完成后，如果知识库启用了图谱索引和提取配置，后处理任务会调用 LLM 从 Chunk 中抽取实体关系并写入 Neo4j。查询时，Pipeline 可从问题中抽取实体，再通过图谱仓储查找关联 Chunk，补充到检索结果中。

## 11. 数据与存储视图

```text
                              +------------------+
                              |  WeKnora Go App  |
                              +--------+---------+
                                       |
        +------------------------------+-------------------------------+
        |                              |                               |
        v                              v                               v
+------------------+         +------------------+           +------------------+
| 业务库           |         | 文件对象         |           | Redis            |
| Tenant/User      |         | 原始文件/预览    |           | 队列/上下文      |
| KB/Knowledge     |         | 解析图片/导出    |           | 流式状态/进度    |
| Chunk/Session    |         +---------+--------+           +---------+--------+
| Message/Model    |                   |                              |
+--------+---------+                   |                              v
         |                             |                    +------------------+
         v                             |                    | Redis            |
+------------------+                   |                    +------------------+
| PostgreSQL       |                   |
| SQLite Lite      |                   v
+------------------+         +------------------+
                             | 对象存储 Provider|
                             | Local FS         |
                             | MinIO / S3       |
                             | Tencent COS      |
                             | Volcengine TOS   |
                             | Aliyun OSS       |
                             +------------------+

        +------------------------------+-------------------------------+
        |                              |
        v                              v
+------------------+         +------------------+
| 检索索引         |         | 图数据库         |
| 向量 + 关键词    |         | 实体/关系        |
+--------+---------+         | 记忆图谱         |
         |                   +---------+--------+
         v                             |
+------------------+                   v
| Postgres pgvector|          +------------------+
| ParadeDB         |          | Neo4j            |
| Elasticsearch    |          +------------------+
| Qdrant           |
| Milvus           |
| Weaviate         |
| SQLite Vec       |
+------------------+
```

数据分工：

- 业务库保存核心元数据、权限、会话、消息、模型、数据源、Wiki 页面等。
- 对象存储保存上传文件、解析图片、导出文件和预览资源。
- 向量/关键词后端保存可检索索引，按知识库配置选择。
- Neo4j 保存 Graph RAG 与记忆图谱数据。
- Redis 保存 asynq 队列、上下文缓存、部分进度和分布式状态。

## 12. 外部接入与集成

### 12.1 数据源同步

`internal/datasource/` 提供通用 Connector 与 Scheduler：

- Connector 适配飞书、Notion、语雀。
- Scheduler 使用 cron 定期触发同步。
- 同步任务写入 asynq 队列，避免阻塞 HTTP 请求。
- 同步日志记录运行状态、错误和完成时间。

### 12.2 IM 集成

`internal/im/` 提供 IM 统一适配层，支持多平台回调、长连接、流式回复、斜杠命令、引用上下文、线程会话、限流与 QA 队列。IM Handler 接收到消息后复用同一套 Session/Agent/RAG 服务，因此 IM 与 Web 的问答能力保持一致。

### 12.3 MCP 集成

MCP 有两类方向：

- WeKnora 作为 MCP Client：`internal/mcp/` 管理租户配置的 MCP 服务连接，Agent 可将 MCP 工具注册进 ToolRegistry。
- WeKnora 作为 MCP Server：`mcp-server/` 是独立 Python 服务，向外部 MCP Client 暴露 WeKnora 知识库、知识、模型、会话和聊天工具。

## 13. 部署架构

### 13.1 Docker Compose

`docker-compose.yml` 定义主要服务：

- `frontend`：Nginx 托管前端并代理后端。
- `app`：Go 后端。
- `docreader`：Python gRPC 文档解析服务。
- `postgres`：业务数据库，默认使用 ParadeDB 镜像。
- `redis`：任务队列与缓存。
- 可选 Profile：`minio`、`qdrant`、`milvus`、`weaviate`、`neo4j`、`jaeger`、`langfuse`、`dex`。
- `sandbox`：Agent Skills 按需执行镜像。

### 13.2 Kubernetes Helm

`helm/` 提供 Kubernetes 部署模板，覆盖 app、frontend、docreader、postgres、redis、neo4j、PVC、Ingress、Secrets 等资源。适合私有云或集群化部署。

### 13.3 Lite/Desktop

`cmd/desktop/` 使用 Wails 启动桌面壳，并在本地启动 Go 后端。桌面模式倾向于使用 SQLite、本地存储、随机或配置端口、内嵌前端资源，适合单机轻量体验。

## 14. 可观测性与运维

系统可观测性由三部分组成：

- 日志：统一 logger、请求 ID、语言上下文。
- OpenTelemetry：可将 Trace 导出到 Jaeger。
- Langfuse：追踪 Agent ReAct、LLM 调用、Token、工具调用和 asynq 任务链路。

异步任务具备队列优先级：

- `critical`：高优先级任务。
- `default`：默认任务。
- `low`：数据源同步等低优先级任务。

常见长任务包括文档解析、FAQ 导入、摘要生成、问题生成、知识库复制、知识移动、索引删除、KB 删除、图片多模态处理、Wiki 生成和数据源同步。

## 15. 扩展点

| 扩展方向 | 主要入口 | 设计方式 |
|----------|----------|----------|
| 新模型厂商 | `internal/models/provider/` 与各能力目录 | 实现 Chat/Embedding/Rerank/VLM/ASR 适配 |
| 新向量库/关键词库 | `internal/application/repository/retriever/`、`retriever.Registry` | 实现检索仓储并注册到 RetrieveEngineRegistry |
| 新对象存储 | `internal/application/service/file/` | 实现 FileService Provider |
| 新文档解析引擎 | `docreader/parser/registry.py` 或 Go 侧 docparser engine | 注册解析器并暴露给知识库解析配置 |
| 新数据源 | `internal/datasource/connector/` | 实现 Connector 接口并注册 |
| 新 IM 平台 | `internal/im/<platform>/` | 实现 Adapter 与 Factory |
| 新 Agent 工具 | `internal/agent/tools/` | 实现 Tool 并注册到 ToolRegistry |
| 新 MCP 工具源 | `internal/mcp/`、租户 MCP 配置 | 通过 SSE/HTTP Streamable MCP 服务动态注册 |
| 新前端功能页 | `frontend/src/views/`、`frontend/src/api/`、`frontend/src/router/` | 按 Vue Router + API 模块扩展 |

## 16. 关键质量属性

### 16.1 可替换性

模型、向量库、对象存储、文档解析、Web Search、MCP 服务都通过接口或注册表解耦。业务层依赖接口，部署层通过环境变量和系统配置选择具体实现。

### 16.2 可伸缩性

耗时操作通过 asynq 任务队列异步执行；检索后端、对象存储和 DocReader 可独立部署；Redis 支撑多实例任务去重、流式状态和上下文缓存。

### 16.3 多租户隔离

核心数据访问以租户 ID 为边界，API Key 与 JWT 共同支撑服务端和用户端访问。共享空间和共享 Agent 通过显式授权扩展访问范围。

### 16.4 安全性

系统具备认证中间件、API Key、JWT、敏感字段加密、SSRF 保护、预签名文件访问、MCP stdio 禁用、Skills 沙盒执行等安全设计。

### 16.5 可观测性

请求、任务、LLM、Agent、工具调用可通过日志、OpenTelemetry 和 Langfuse 串联排查。SSE 事件也会通过 StreamManager 支撑断线续传和状态恢复。

## 17. 顶层设计结论

WeKnora 的架构核心是“一个 Go 应用编排多个可插拔能力后端”。Go App 承担 API、认证、业务编排、任务调度、检索流水线和 Agent 运行时；DocReader、模型服务、向量数据库、对象存储、Neo4j、Redis、MCP、IM 和数据源连接器围绕它形成可替换的能力网络。

这种架构适合两类部署：

- 轻量单机：SQLite、本地文件、内嵌前端、同步任务或本地 Redis，降低体验门槛。
- 企业私有化：PostgreSQL/Redis/DocReader/向量库/对象存储/Neo4j/可观测栈独立部署，支持更高并发、更强检索和更完整运维能力。
