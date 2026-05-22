# RAG + Neo4j + Wiki 多路召回改造说明

## 改造原因

原快速问答模式已经支持分块 RAG 与 Neo4j 知识图谱并行召回：分块 RAG 负责精确证据，Neo4j 图谱负责实体关系扩展。但 Wiki 页面是另一类重要知识表面，适合回答概念解释、主题导航、跨文档摘要和关系脉络类问题。

如果只依赖 RAG + Neo4j：

- 对“某概念是什么、有哪些相关主题、整体脉络如何”这类问题，容易缺少 Wiki 的高层组织信息。
- 对已经生成 Wiki 的知识库，Wiki 页面不会主动参与快速问答召回，只能在智能推理模式中由模型调用 wiki 工具。
- 同一问题可能需要“原文证据 + 图谱关系 + Wiki 总结”共同支撑，单一路径召回不稳定。

本次改造提供一个可配置的快速问答增强能力：在快速问答模式中开启后，系统会同时执行 RAG 分块检索、Neo4j 图谱检索和 Wiki 页面检索，再合并去重后进入后续重排与答案生成。

## 改造内容

### 1. 智能体配置新增开关

自定义智能体配置新增两个字段：

| 字段 | 含义 | 默认值 |
|---|---|---|
| `multi_route_retrieval_enabled` | 是否启用 RAG + Neo4j + Wiki 多路召回 | `false` |
| `wiki_recall_top_k` | 每个知识库召回的 Wiki 页面候选数 | `5` |

前端“创建/编辑智能体 -> 检索策略”中，在快速问答模式下可以配置这两个字段。

### 2. 快速问答并行召回链路增加 Wiki 分支

`CHUNK_SEARCH_PARALLEL` 现在包含三类召回：

```text
用户问题
  |
  v
QUERY_UNDERSTAND
  |
  v
CHUNK_SEARCH_PARALLEL
  |-- chunk_search  -> RAG 分块检索（向量 + 关键词）
  |-- entity_search -> Neo4j 知识图谱实体关系召回
  |-- wiki_search   -> Wiki 页面召回（仅开关开启时）
  |
  v
合并、去重、保留来源 metadata
  |
  v
CHUNK_RERANK -> CHUNK_MERGE -> FILTER_TOP_K -> 生成答案
```

### 3. Wiki 页面转换为统一 SearchResult

Wiki 命中页会被转换成现有 `SearchResult`：

- 优先使用 `WikiPage.ChunkRefs` 找回原始文档 chunk，作为可追溯证据。
- 若 Wiki 页没有 `ChunkRefs`，回退为虚拟 Wiki 结果，内容由标题、摘要和页面正文组成。
- 统一标记 `MatchTypeWikiPage`。
- metadata 中写入：
  - `retrieval_source=wiki`
  - `wiki_page_id`
  - `wiki_page_slug`
  - `wiki_page_title`
  - `wiki_page_type`
  - `wiki_page_summary`（如存在）

如果同一个 chunk 同时被 RAG 或 Wiki 命中，系统会合并 Wiki metadata，不会直接丢弃 Wiki 命中信息。

### 4. 租户与文档范围保护

多路召回继续遵守现有租户和共享知识库边界：

- `SearchTarget.TenantID` 会在并行分支 Clone 时保留。
- Wiki repository 搜索会消费上下文中的 `tenant_id`，避免仅按 `knowledge_base_id` 查询。
- 共享知识库、共享 Agent 场景下，Wiki 工具和快速问答 Wiki 分支会使用目标知识库所属租户执行查询。
- 当用户通过 `@` 指定某些文档时，Wiki 分支会按 `SourceRefs` 和 `ChunkRefs` 中的 `KnowledgeID` 做过滤；若 Wiki 页有 chunk 引用但无法确认属于目标文档，则不回退整页内容，避免越界召回。

## 使用方法

### 前提条件

要获得完整效果，目标知识库需要具备相应能力：

1. RAG 分块检索：知识库启用向量或关键词索引。
2. Neo4j 图谱召回：环境开启 `NEO4J_ENABLE`，且知识库或文档开启实体关系抽取。
3. Wiki 召回：知识库启用 Wiki，并已完成 Wiki 生成。

未满足的分支会自然无结果，不会影响其他分支召回。

### 控制台配置

1. 进入“智能体”页面。
2. 创建或编辑一个智能体。
3. 运行模式选择“快速问答”。
4. 进入“知识库”，绑定需要检索的知识库。
5. 进入“检索策略”。
6. 打开“RAG + Neo4j + Wiki 多路召回”。
7. 设置“Wiki 召回数量”，建议从默认 `5` 开始。
8. 保存智能体。

### API 配置

创建或更新智能体时，在 `config` 中加入：

```json
{
  "agent_mode": "quick-answer",
  "multi_route_retrieval_enabled": true,
  "wiki_recall_top_k": 5
}
```

### 调优建议

- `wiki_recall_top_k=3~5`：适合大多数问答场景，延迟和上下文体积较稳。
- `wiki_recall_top_k=6~10`：适合概念解释、跨文档分析、主题导航类问题。
- 不建议长期设置过高。Wiki 命中会进入后续重排和上下文组装，过多候选会增加延迟和上下文噪声。

### 适用场景

建议开启：

- 知识库已经启用 Wiki，并且用户经常问概念、关系、摘要、脉络类问题。
- 问题需要同时依赖原文证据和 Wiki 汇总。
- 希望快速问答模式具备接近“RAG + 图谱 + Wiki”的自动召回能力，而不是依赖智能推理模式手动调用工具。

可以保持关闭：

- 知识库没有 Wiki。
- 主要是精确原文查找或 FAQ 问答。
- 对最低延迟要求高，且当前 RAG + Neo4j 已满足召回质量。
