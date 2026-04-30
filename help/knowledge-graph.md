# WeKnora 知识图谱 (Graph RAG) 功能指南

## 架构总览

```
ENABLE_GRAPH_RAG (前端展示开关)
       ↓
NEO4J_ENABLE=true (后端实际开关)
       ↓
知识库 ExtractConfig.Enabled=true + 配置实体/关系模板
       ↓
触发：文档上传 → 逐Chunk提取实体关系 → 存入Neo4j
查询：用户提问 → LLM提取实体名 → Neo4j搜索关联Chunk → 增强回答
```

---

## 一、前置条件

### 1. 启动 Neo4j 容器

Neo4j 定义在 `docker-compose.dev.yml` 中，需要显式指定 profile 启动：

```bash
docker compose -f docker-compose.dev.yml --profile neo4j up -d
```

容器信息：
- 镜像：`neo4j:latest`
- 容器名：`WeKnora-neo4j-dev`
- Bolt 端口：`7687`
- HTTP 端口：`7474`

### 2. 配置 .env

```env
# 全局开关（前端展示用）
ENABLE_GRAPH_RAG=true

# Neo4j 连接配置（后端实际开关）
NEO4J_ENABLE=true
NEO4J_URI=neo4j://localhost:7687    # go run 在宿主机运行，用 localhost
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=password
```

> **注意**：生产环境请修改默认密码。

### 3. 重启后端服务

```bash
./scripts/run-dev.sh
```

### 4. 验证配置生效

```bash
curl -s http://localhost:8080/api/v1/system/info | python3 -m json.tool | grep graph
```

应返回 `"graph_database_engine": "neo4j"`。

---

## 二、图谱创建流程

### 触发条件

图谱提取需要**三个条件同时满足**：

| 条件 | 位置 | 说明 |
|------|------|------|
| `NEO4J_ENABLE=true` | `.env` | 后端全局开关 |
| `IndexingStrategy.GraphEnabled=true` | 知识库配置 | 每个知识库独立控制 |
| `ExtractConfig.Enabled=true` | 知识库配置 | 需配置实体/关系模板 |

源码位置：[container.go:1091](internal/container/container.go#L1091)、[knowledgebase.go:595-600](internal/types/knowledgebase.go#L595-L600)

### 提取流程

```
文档上传
  → 文档解析分块 (Chunk)
  → 向量索引完成
  → KnowledgePostProcess 检查 kb.IsGraphEnabled()
  → 为每个 Chunk 创建 ChunkExtractTask
  → LLM 根据 ExtractConfig 模板提取实体和关系
  → 存入 Neo4j
```

### ExtractConfig 配置要素

在创建/编辑知识库时，需配置以下四项：

- **Text** — 示例文本，让 LLM 理解提取目标
- **Tags** — 关系类型标签，如 `["属于", "依赖", "调用", "包含"]`
- **Nodes** — 示例实体节点列表
- **Relations** — 示例关系列表

### 关键源码

| 阶段 | 文件 | 行号 |
|------|------|------|
| 后处理入口 | [knowledge_post_process.go](internal/application/service/knowledge_post_process.go) | :117 |
| 提取任务创建 | [extract.go](internal/application/service/extract.go) | :84 |
| LLM 提取执行 | [extract.go](internal/application/service/extract.go) | :219-237 |
| 图数据写入 Neo4j | [extract.go](internal/application/service/extract.go) | :234-237 |

---

## 三、基于图谱的查询流程

### 查询流程

```
用户提问
  → QUERY_UNDERSTAND: PluginExtractEntity
      LLM 从问题中提取实体名
  → CHUNK_SEARCH_PARALLEL: 并行执行
      ├── 向量检索 (Qdrant)
      └── 图谱检索 (Neo4j): 用实体名搜索关联 Chunk
  → RERANK: 结果合并重排序
  → CHAT_COMPLETION: LLM 生成回答
```

Pipeline 定义：[chat_manage.go:303-312](internal/types/chat_manage.go#L303-L312)

### 图谱检索生效条件

图谱检索**只在用户提问中能提取出实体名时才生效**。如果问题不涉及具体实体（如 "总结一下"），图谱不会参与检索。

### 关键源码

| 阶段 | 文件 | 行号 |
|------|------|------|
| 查询实体提取 | [extract_entity.go](internal/application/service/chat_pipeline/extract_entity.go) | :57 |
| 并行搜索编排 | [search_parallel.go](internal/application/service/chat_pipeline/search_parallel.go) | :117-151 |
| Neo4j 图谱搜索 | [search_entity.go](internal/application/service/chat_pipeline/search_entity.go) | :75-126 |

---

## 四、测试指南

### 测试图谱提取效果

调用初始化 API 测试 LLM 从示例文本中提取实体关系的能力：

```
POST /api/v1/initialization/extract/text-relation
```

此 API 接受示例文本和 ExtractConfig，返回提取的结构化结果，用于配置前验证效果。

源码：[initialization.go:2162](internal/handler/initialization.go#L2162)

### 端到端测试

1. 进入前端系统设置 → 知识库 → 新建
2. 填写「知识图谱」配置段：示例文本、关系标签、示例实体和关系
3. 上传一个包含实体关系的文档（如技术方案、架构说明、组织架构图描述）
4. 等待文档处理完成（文档状态变为完成）
5. 进入聊天，选择该知识库
6. 提问涉及具体实体的问题，如「X 模块依赖哪些组件？」
7. 观察回答是否结合了图谱检索结果

---

## 五、配置检查清单

| 检查项 | 命令/方式 |
|--------|-----------|
| Neo4j 容器运行 | `docker ps \| grep neo4j` |
| Neo4j 端口可达 | `nc -z localhost 7687` |
| .env NEO4J_ENABLE | `grep NEO4J_ENABLE .env` |
| .env ENABLE_GRAPH_RAG | `grep ENABLE_GRAPH_RAG .env` |
| 系统信息确认 | `curl -s localhost:8080/api/v1/system/info` |
| 知识库已配置 ExtractConfig | 前端知识库设置页面检查 |
