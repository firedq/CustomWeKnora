# RAG Neo4j Wiki Multi-Route Retrieval Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an opt-in quick-answer retrieval pipeline that recalls evidence from RAG chunks, Neo4j entity graph results, and Wiki pages in parallel, then fuses the results before the existing rerank and answer stages.

**Architecture:** Keep `HybridSearch` unchanged as vector + keyword retrieval. Extend the existing `CHUNK_SEARCH_PARALLEL` stage with an optional Wiki branch controlled by persisted agent configuration, reuse the existing graph branch, and return all branches as normal `SearchResult` values so existing rerank, merge, citation, and fallback behavior continue to work. The feature is disabled by default to preserve upstream behavior and reduce future merge conflicts.

**Tech Stack:** Go chat pipeline plugins, existing WeKnora `types.SearchResult` and `types.WikiPage` models, PostgreSQL-backed Wiki search, Neo4j graph repository, Vue 3 + TDesign agent editor, existing Go tests.

---

## Branch And Upgrade Constraints

- Do not create a new branch. Continue working on the current feature branch.
- Prefer new files and narrowly scoped edits so future upstream merges touch as few shared files as possible.
- Do not change the meaning of `knowledgeBaseService.HybridSearch`; it must remain vector + keyword retrieval.
- The new behavior must be opt-in. Existing quick-answer agents continue using the current RAG + graph pipeline unless `multi_route_retrieval_enabled` is set.

## File Structure

- Modify `internal/types/custom_agent.go`: persist quick-answer agent flags for multi-route retrieval and Wiki recall top K.
- Modify `internal/types/chat_manage.go`: carry the new runtime flags through `PipelineRequest` and `Clone`.
- Modify `internal/types/embedding.go`: add a new match type for Wiki-page recall without changing existing numeric values.
- Modify `internal/agent/tools/tool.go`: render the new match type in debug/tool displays.
- Modify `internal/application/service/session_qa_helpers.go`: apply custom-agent retrieval settings onto `ChatManage`.
- Create `internal/application/service/chat_pipeline/search_wiki.go`: Wiki recall branch that converts matching Wiki pages into `SearchResult` values, preferring source chunks from `ChunkRefs`.
- Create `internal/application/service/chat_pipeline/search_wiki_test.go`: unit tests for Wiki recall conversion and filtering.
- Modify `internal/application/service/chat_pipeline/search_parallel.go`: add the optional `wiki_search` task and merge route metrics.
- Create or modify `internal/application/service/chat_pipeline/search_parallel_test.go`: unit test that the Wiki branch only runs when enabled and merges with RAG/graph results.
- Modify `frontend/src/api/agent/index.ts`: expose new config fields to the frontend type.
- Modify `frontend/src/views/agent/AgentEditorModal.vue`: add retrieval strategy controls in the existing “检索策略” section.
- Modify frontend locale files at least `frontend/src/i18n/locales/zh-CN.ts` and `frontend/src/i18n/locales/en-US.ts`: labels and descriptions for the new controls.
- Create `help/optimization/rag-neo4j-wiki-multiroute-retrieval.md`: user-facing document covering reason, changes, and usage.

---

### Task 1: Add Persisted And Runtime Feature Flags

**Files:**
- Modify: `internal/types/custom_agent.go:205-215`
- Modify: `internal/types/chat_manage.go:13-70`
- Modify: `internal/types/chat_manage.go:182-224`
- Modify: `internal/application/service/session_qa_helpers.go:140-213`
- Test: `internal/types/chat_manage_test.go`

- [ ] **Step 1: Write the failing runtime clone test**

Create `internal/types/chat_manage_test.go` with:

```go
package types

import "testing"

func TestChatManageClonePreservesMultiRouteRetrievalConfig(t *testing.T) {
	cm := &ChatManage{
		PipelineRequest: PipelineRequest{
			Query:                      "how are A and B related",
			KnowledgeBaseIDs:           []string{"kb-1"},
			SearchTargets:              SearchTargets{{Type: SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 7}},
			MultiRouteRetrievalEnabled: true,
			WikiRecallTopK:             6,
		},
		PipelineState: PipelineState{
			Entity:      []string{"A"},
			EntityKBIDs: []string{"kb-1"},
		},
	}

	clone := cm.Clone()
	if !clone.MultiRouteRetrievalEnabled {
		t.Fatalf("expected MultiRouteRetrievalEnabled to be preserved")
	}
	if clone.WikiRecallTopK != 6 {
		t.Fatalf("expected WikiRecallTopK=6, got %d", clone.WikiRecallTopK)
	}
	if len(clone.SearchTargets) != 1 || clone.SearchTargets[0].TenantID != 7 {
		t.Fatalf("expected cloned search target tenant id 7, got %#v", clone.SearchTargets)
	}
}
```

- [ ] **Step 2: Run the failing test**

Run:

```bash
go test ./internal/types -run TestChatManageClonePreservesMultiRouteRetrievalConfig -count=1
```

Expected: FAIL because `PipelineRequest` does not yet define `MultiRouteRetrievalEnabled` and `WikiRecallTopK`.

- [ ] **Step 3: Add fields to `CustomAgentConfig`**

In `internal/types/custom_agent.go`, inside the retrieval strategy section after `RerankThreshold`, add:

```go
	// MultiRouteRetrievalEnabled enables the quick-answer pipeline to add Wiki page
	// recall alongside the existing RAG chunk and Neo4j graph recall branches.
	// It is opt-in so existing agents keep the current retrieval behavior.
	MultiRouteRetrievalEnabled bool `yaml:"multi_route_retrieval_enabled" json:"multi_route_retrieval_enabled"`
	// WikiRecallTopK limits the number of Wiki pages searched per Wiki-enabled KB
	// when multi-route retrieval is enabled. Zero uses the runtime default.
	WikiRecallTopK int `yaml:"wiki_recall_top_k" json:"wiki_recall_top_k"`
```

- [ ] **Step 4: Add fields to `PipelineRequest`**

In `internal/types/chat_manage.go`, inside `PipelineRequest` after `Language`, add:

```go
	// MultiRouteRetrievalEnabled enables optional Wiki page recall in the quick-answer
	// retrieval pipeline. RAG chunk recall and Neo4j graph recall remain unchanged.
	MultiRouteRetrievalEnabled bool `json:"-"`
	// WikiRecallTopK limits Wiki page hits per Wiki-enabled KB when multi-route
	// retrieval is enabled. Zero is normalized by the Wiki recall branch.
	WikiRecallTopK int `json:"-"`
```

- [ ] **Step 5: Preserve fields in `ChatManage.Clone`**

In the `PipelineRequest` literal inside `ChatManage.Clone`, add:

```go
			MultiRouteRetrievalEnabled: c.MultiRouteRetrievalEnabled,
			WikiRecallTopK:             c.WikiRecallTopK,
```

Also preserve `TenantID` on cloned `SearchTarget` values by changing the target copy to:

```go
			searchTargets[i] = &SearchTarget{
				Type:            t.Type,
				KnowledgeBaseID: t.KnowledgeBaseID,
				TenantID:        t.TenantID,
				KnowledgeIDs:    kidsCopy,
			}
```

- [ ] **Step 6: Apply custom-agent overrides**

In `internal/application/service/session_qa_helpers.go`, after the existing rerank override block, add:

```go
	cm.MultiRouteRetrievalEnabled = customAgent.Config.MultiRouteRetrievalEnabled
	if customAgent.Config.WikiRecallTopK > 0 {
		cm.WikiRecallTopK = customAgent.Config.WikiRecallTopK
	}
	if cm.MultiRouteRetrievalEnabled {
		logger.Infof(ctx, "Multi-route retrieval enabled by custom agent, wiki_recall_top_k=%d", cm.WikiRecallTopK)
	}
```

- [ ] **Step 7: Run the type tests**

Run:

```bash
go test ./internal/types -run TestChatManageClonePreservesMultiRouteRetrievalConfig -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/types/custom_agent.go internal/types/chat_manage.go internal/types/chat_manage_test.go internal/application/service/session_qa_helpers.go
git commit -m "feat: add multi-route retrieval config flags"
```

---

### Task 2: Add Wiki Recall Result Conversion

**Files:**
- Create: `internal/application/service/chat_pipeline/search_wiki.go`
- Create: `internal/application/service/chat_pipeline/search_wiki_test.go`
- Modify: `internal/types/embedding.go:12-26`
- Modify: `internal/agent/tools/tool.go:65-83`

- [ ] **Step 1: Write the failing Wiki conversion tests**

Create `internal/application/service/chat_pipeline/search_wiki_test.go` with:

```go
package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestWikiPagesToSearchResultsPrefersChunkRefs(t *testing.T) {
	page := &types.WikiPage{
		ID:              "page-1",
		TenantID:        9,
		KnowledgeBaseID: "kb-1",
		Slug:            "entity/service-a",
		Title:           "Service A",
		PageType:        types.WikiPageTypeEntity,
		Content:         "Service A depends on Service B.",
		Summary:         "Service A dependency summary.",
		ChunkRefs:       types.StringArray{"chunk-1"},
		SourceRefs:      types.StringArray{"doc-1|Architecture"},
	}
	chunk := &types.Chunk{
		ID:              "chunk-1",
		Content:         "Original document says Service A calls Service B over HTTP.",
		KnowledgeID:     "doc-1",
		KnowledgeBaseID: "kb-1",
		ChunkIndex:      3,
	}
	knowledge := &types.Knowledge{
		ID:              "doc-1",
		KnowledgeBaseID: "kb-1",
		Title:           "Architecture",
		FileName:        "architecture.md",
	}

	results := wikiPagesToSearchResults(context.Background(), []*types.WikiPage{page}, map[string]*types.Chunk{"chunk-1": chunk}, map[string]*types.Knowledge{"doc-1": knowledge})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	result := results[0]
	if result.ID != "chunk-1" {
		t.Fatalf("expected result ID chunk-1, got %q", result.ID)
	}
	if result.MatchType != types.MatchTypeWikiPage {
		t.Fatalf("expected MatchTypeWikiPage, got %v", result.MatchType)
	}
	if result.Content != chunk.Content {
		t.Fatalf("expected original chunk content, got %q", result.Content)
	}
	if result.Metadata["wiki_slug"] != "entity/service-a" {
		t.Fatalf("expected wiki_slug metadata, got %#v", result.Metadata)
	}
}

func TestWikiPagesToSearchResultsFallsBackToVirtualWikiResult(t *testing.T) {
	page := &types.WikiPage{
		ID:              "page-2",
		TenantID:        9,
		KnowledgeBaseID: "kb-1",
		Slug:            "concept/retrieval",
		Title:           "Retrieval",
		PageType:        types.WikiPageTypeConcept,
		Content:         "Retrieval combines several search surfaces.",
		Summary:         "Retrieval overview.",
		SourceRefs:      types.StringArray{"doc-2|Search Design"},
	}

	results := wikiPagesToSearchResults(context.Background(), []*types.WikiPage{page}, nil, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 virtual result, got %d", len(results))
	}
	result := results[0]
	if result.ID != "wiki-page-2" {
		t.Fatalf("expected virtual ID wiki-page-2, got %q", result.ID)
	}
	if result.ChunkType != string(types.ChunkTypeWikiPage) {
		t.Fatalf("expected wiki_page chunk type, got %q", result.ChunkType)
	}
	if result.KnowledgeBaseID != "kb-1" {
		t.Fatalf("expected KB kb-1, got %q", result.KnowledgeBaseID)
	}
}
```

- [ ] **Step 2: Run the failing tests**

Run:

```bash
go test ./internal/application/service/chat_pipeline -run 'TestWikiPagesToSearchResults' -count=1
```

Expected: FAIL because `wikiPagesToSearchResults` and `types.MatchTypeWikiPage` do not exist.

- [ ] **Step 3: Add a Wiki match type without changing existing enum values**

In `internal/types/embedding.go`, append a new constant after `MatchTypeDataAnalysis`:

```go
	MatchTypeWikiPage // Wiki page recall match type
```

Do not insert it in the middle of the existing list, because existing numeric values may be serialized in logs or stored JSON.

- [ ] **Step 4: Add display formatting for the new match type**

In `internal/agent/tools/tool.go`, add a switch case:

```go
	case types.MatchTypeWikiPage:
		return "Wiki Page Match"
```

- [ ] **Step 5: Add Wiki conversion helpers**

Create `internal/application/service/chat_pipeline/search_wiki.go` with:

```go
package chatpipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const defaultWikiRecallTopK = 5
const maxWikiRecallTopK = 20

type PluginSearchWiki struct {
	wikiService   interfaces.WikiPageService
	chunkRepo     interfaces.ChunkRepository
	knowledgeRepo interfaces.KnowledgeRepository
}

func NewPluginSearchWiki(
	wikiService interfaces.WikiPageService,
	chunkRepo interfaces.ChunkRepository,
	knowledgeRepo interfaces.KnowledgeRepository,
) *PluginSearchWiki {
	return &PluginSearchWiki{
		wikiService:   wikiService,
		chunkRepo:     chunkRepo,
		knowledgeRepo: knowledgeRepo,
	}
}

func (p *PluginSearchWiki) Search(ctx context.Context, chatManage *types.ChatManage) []*types.SearchResult {
	if p == nil || p.wikiService == nil || !chatManage.MultiRouteRetrievalEnabled {
		return nil
	}
	if !chatManage.NeedsRetrieval() {
		return nil
	}

	query := strings.TrimSpace(chatManage.RewriteQuery)
	if query == "" {
		query = strings.TrimSpace(chatManage.Query)
	}
	if query == "" {
		return nil
	}

	limit := normalizeWikiRecallTopK(chatManage.WikiRecallTopK)
	kbIDs := uniqueWikiRecallKBIDs(chatManage.SearchTargets, chatManage.KnowledgeBaseIDs)
	if len(kbIDs) == 0 {
		return nil
	}

	pages := make([]*types.WikiPage, 0)
	for _, kbID := range kbIDs {
		hits, err := p.wikiService.SearchPages(ctx, kbID, query, limit)
		if err != nil {
			logger.Warnf(ctx, "[WikiSearch] failed to search wiki pages for KB %s: %v", kbID, err)
			continue
		}
		pages = append(pages, hits...)
	}
	if len(pages) == 0 {
		return nil
	}

	chunkMap := p.loadWikiChunkRefs(ctx, chatManage.TenantID, pages)
	knowledgeMap := p.loadKnowledgeForChunks(ctx, chatManage.TenantID, chunkMap)
	results := wikiPagesToSearchResults(ctx, pages, chunkMap, knowledgeMap)
	logger.Infof(ctx, "[WikiSearch] page_hits=%d result_count=%d", len(pages), len(results))
	return results
}

func normalizeWikiRecallTopK(v int) int {
	if v <= 0 {
		return defaultWikiRecallTopK
	}
	if v > maxWikiRecallTopK {
		return maxWikiRecallTopK
	}
	return v
}

func uniqueWikiRecallKBIDs(searchTargets types.SearchTargets, fallback []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0)
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, target := range searchTargets {
		if target != nil {
			add(target.KnowledgeBaseID)
		}
	}
	if len(out) == 0 {
		for _, id := range fallback {
			add(id)
		}
	}
	sort.Strings(out)
	return out
}

func (p *PluginSearchWiki) loadWikiChunkRefs(ctx context.Context, tenantID uint64, pages []*types.WikiPage) map[string]*types.Chunk {
	ids := make([]string, 0)
	seen := map[string]bool{}
	for _, page := range pages {
		for _, id := range page.ChunkRefs {
			if id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) == 0 || p.chunkRepo == nil {
		return nil
	}
	chunks, err := p.chunkRepo.ListChunksByID(ctx, tenantID, ids)
	if err != nil {
		logger.Warnf(ctx, "[WikiSearch] failed to load wiki chunk refs: %v", err)
		return nil
	}
	out := make(map[string]*types.Chunk, len(chunks))
	for _, chunk := range chunks {
		if chunk != nil {
			out[chunk.ID] = chunk
		}
	}
	return out
}

func (p *PluginSearchWiki) loadKnowledgeForChunks(ctx context.Context, tenantID uint64, chunkMap map[string]*types.Chunk) map[string]*types.Knowledge {
	if len(chunkMap) == 0 || p.knowledgeRepo == nil {
		return nil
	}
	seen := map[string]bool{}
	ids := make([]string, 0)
	for _, chunk := range chunkMap {
		if chunk.KnowledgeID != "" && !seen[chunk.KnowledgeID] {
			seen[chunk.KnowledgeID] = true
			ids = append(ids, chunk.KnowledgeID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	knowledges, err := p.knowledgeRepo.GetKnowledgeBatch(ctx, tenantID, ids)
	if err != nil {
		logger.Warnf(ctx, "[WikiSearch] failed to load wiki source knowledges: %v", err)
		return nil
	}
	out := make(map[string]*types.Knowledge, len(knowledges))
	for _, knowledge := range knowledges {
		if knowledge != nil {
			out[knowledge.ID] = knowledge
		}
	}
	return out
}

func wikiPagesToSearchResults(
	ctx context.Context,
	pages []*types.WikiPage,
	chunkMap map[string]*types.Chunk,
	knowledgeMap map[string]*types.Knowledge,
) []*types.SearchResult {
	results := make([]*types.SearchResult, 0, len(pages))
	seenResultIDs := map[string]bool{}
	for _, page := range pages {
		if page == nil || page.Status == types.WikiPageStatusArchived {
			continue
		}

		emittedChunk := false
		for _, chunkID := range page.ChunkRefs {
			chunk := chunkMap[chunkID]
			if chunk == nil || seenResultIDs[chunk.ID] {
				continue
			}
			knowledge := knowledgeMap[chunk.KnowledgeID]
			results = append(results, wikiChunkToSearchResult(chunk, knowledge, page))
			seenResultIDs[chunk.ID] = true
			emittedChunk = true
		}
		if emittedChunk {
			continue
		}

		virtualID := "wiki-" + page.ID
		if seenResultIDs[virtualID] {
			continue
		}
		results = append(results, wikiPageToVirtualSearchResult(ctx, page, virtualID))
		seenResultIDs[virtualID] = true
	}
	return results
}

func wikiChunkToSearchResult(chunk *types.Chunk, knowledge *types.Knowledge, page *types.WikiPage) *types.SearchResult {
	metadata := map[string]string{
		"source_route":    "wiki",
		"wiki_page_id":   page.ID,
		"wiki_slug":      page.Slug,
		"wiki_title":     page.Title,
		"wiki_page_type": page.PageType,
	}
	result := &types.SearchResult{
		ID:              chunk.ID,
		Content:         chunk.Content,
		KnowledgeID:     chunk.KnowledgeID,
		ChunkIndex:      chunk.ChunkIndex,
		StartAt:         chunk.StartAt,
		EndAt:           chunk.EndAt,
		Seq:             chunk.ChunkIndex,
		Score:           0.85,
		MatchType:       types.MatchTypeWikiPage,
		Metadata:        metadata,
		ChunkType:       string(chunk.ChunkType),
		ParentChunkID:   chunk.ParentChunkID,
		ImageInfo:       chunk.ImageInfo,
		ChunkMetadata:   chunk.Metadata,
		KnowledgeBaseID: chunk.KnowledgeBaseID,
	}
	if knowledge != nil {
		result.KnowledgeTitle = knowledge.Title
		result.KnowledgeFilename = knowledge.FileName
		result.KnowledgeSource = knowledge.Source
		result.KnowledgeChannel = knowledge.Channel
		result.KnowledgeDescription = knowledge.Description
		result.Metadata = mergeWikiMetadata(result.Metadata, knowledge.GetMetadata())
	}
	return result
}

func wikiPageToVirtualSearchResult(ctx context.Context, page *types.WikiPage, id string) *types.SearchResult {
	content := strings.TrimSpace(page.Content)
	if content == "" {
		content = strings.TrimSpace(page.Summary)
	}
	if content == "" {
		content = page.Title
	}
	knowledgeID := firstSourceKnowledgeID(page.SourceRefs)
	if knowledgeID == "" {
		logger.Debugf(ctx, "[WikiSearch] page %s has no source refs, emitting virtual wiki result", page.Slug)
	}
	return &types.SearchResult{
		ID:              id,
		Content:         content,
		KnowledgeID:     knowledgeID,
		KnowledgeTitle:  page.Title,
		Score:           0.75,
		MatchType:       types.MatchTypeWikiPage,
		Metadata:        map[string]string{"source_route": "wiki", "wiki_page_id": page.ID, "wiki_slug": page.Slug, "wiki_title": page.Title, "wiki_page_type": page.PageType},
		ChunkType:       string(types.ChunkTypeWikiPage),
		KnowledgeBaseID: page.KnowledgeBaseID,
	}
}

func firstSourceKnowledgeID(refs []string) string {
	if len(refs) == 0 {
		return ""
	}
	ref := refs[0]
	if i := strings.Index(ref, "|"); i > 0 {
		return ref[:i]
	}
	return ref
}

func mergeWikiMetadata(base map[string]string, extra map[string]string) map[string]string {
	if base == nil {
		base = map[string]string{}
	}
	for k, v := range extra {
		if _, exists := base[k]; !exists {
			base[k] = v
		}
	}
	return base
}

func formatWikiRouteStats(route string, count int) string {
	return fmt.Sprintf("%s_results=%d", route, count)
}
```

- [ ] **Step 6: Run the Wiki conversion tests**

Run:

```bash
go test ./internal/application/service/chat_pipeline -run 'TestWikiPagesToSearchResults' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/types/embedding.go internal/agent/tools/tool.go internal/application/service/chat_pipeline/search_wiki.go internal/application/service/chat_pipeline/search_wiki_test.go
git commit -m "feat: add wiki recall conversion"
```

---

### Task 3: Wire Wiki Recall Into The Parallel Search Stage

**Files:**
- Modify: `internal/application/service/chat_pipeline/search_parallel.go:12-170`
- Modify: `internal/container/container.go:260-278`
- Test: `internal/application/service/chat_pipeline/search_parallel_test.go`

- [ ] **Step 1: Write a small unit test for the Wiki branch flag**

Create `internal/application/service/chat_pipeline/search_parallel_test.go` with:

```go
package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestSearchParallelSkipsWikiWhenMultiRouteDisabled(t *testing.T) {
	p := &PluginSearchParallel{
		searchWikiPlugin: &PluginSearchWiki{},
	}
	cm := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:            "Service A",
			RewriteQuery:     "",
			KnowledgeBaseIDs: []string{"kb-1"},
		},
		PipelineState: types.PipelineState{
			Intent: types.IntentKBSearch,
		},
	}

	got := p.searchWikiIfEnabled(context.Background(), cm)
	if len(got) != 0 {
		t.Fatalf("expected no wiki results when multi-route retrieval is disabled, got %d", len(got))
	}
}

func TestSearchParallelAllowsWikiWhenMultiRouteEnabled(t *testing.T) {
	p := &PluginSearchParallel{
		searchWikiPlugin: &PluginSearchWiki{},
	}
	cm := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:                      "Service A",
			KnowledgeBaseIDs:           []string{"kb-1"},
			MultiRouteRetrievalEnabled: true,
			WikiRecallTopK:             3,
		},
		PipelineState: types.PipelineState{
			Intent: types.IntentKBSearch,
		},
	}

	got := p.searchWikiIfEnabled(context.Background(), cm)
	if got == nil {
		t.Fatalf("expected an empty slice, not nil, when wiki branch is enabled but has no service")
	}
}
```

- [ ] **Step 2: Run the failing tests**

Run:

```bash
go test ./internal/application/service/chat_pipeline -run 'TestSearchParallel.*Wiki' -count=1
```

Expected: FAIL because `PluginSearchParallel.searchWikiPlugin` and `searchWikiIfEnabled` do not exist.

- [ ] **Step 3: Add Wiki service dependency to `PluginSearchParallel`**

In `internal/application/service/chat_pipeline/search_parallel.go`, add a field:

```go
	searchWikiPlugin *PluginSearchWiki
```

Change the constructor signature to include:

```go
	wikiPageService interfaces.WikiPageService,
```

Create the internal Wiki plugin before constructing `PluginSearchParallel`:

```go
	searchWikiPlugin := NewPluginSearchWiki(wikiPageService, chunkRepository, knowledgeRepository)
```

Add it to the result struct:

```go
		searchWikiPlugin:   searchWikiPlugin,
```

- [ ] **Step 4: Add the helper used by the tests**

In `search_parallel.go`, add:

```go
func (p *PluginSearchParallel) searchWikiIfEnabled(ctx context.Context, cm *types.ChatManage) []*types.SearchResult {
	if !cm.MultiRouteRetrievalEnabled {
		return nil
	}
	if p.searchWikiPlugin == nil {
		return []*types.SearchResult{}
	}
	results := p.searchWikiPlugin.Search(ctx, cm)
	if results == nil {
		return []*types.SearchResult{}
	}
	return results
}
```

- [ ] **Step 5: Add the third parallel task**

In `OnEvent`, after creating `entityCM`, create:

```go
	wikiCM := chatManage.Clone()
	wikiCM.SearchResult = nil
```

Append a third task when `chatManage.MultiRouteRetrievalEnabled` is true:

```go
	if chatManage.MultiRouteRetrievalEnabled {
		tasks = append(tasks, ParallelTask{
			Name: "wiki_search",
			Run: func() *PluginError {
				wikiCM.SearchResult = p.searchWikiIfEnabled(ctx, wikiCM)
				pipelineInfo(ctx, "SearchParallel", "wiki_search_done", map[string]interface{}{
					"result_count": len(wikiCM.SearchResult),
				})
				return nil
			},
		})
	}
```

Change the merge section to:

```go
	chatManage.SearchResult = append(chunkCM.SearchResult, entityCM.SearchResult...)
	chatManage.SearchResult = append(chatManage.SearchResult, wikiCM.SearchResult...)
	chatManage.SearchResult = removeDuplicateResults(chatManage.SearchResult)
```

Add `wiki_results` to the completion log:

```go
		"wiki_results":   len(wikiCM.SearchResult),
```

- [ ] **Step 6: Ensure DI compiles**

Because `interfaces.WikiPageService` is already provided to the container for agent tools, adding it to `NewPluginSearchParallel` should be resolved by dependency injection. Keep `internal/container/container.go` plugin registration order unchanged:

```go
	must(container.Invoke(chatpipeline.NewPluginSearchParallel))
```

- [ ] **Step 7: Run the search pipeline tests**

Run:

```bash
go test ./internal/application/service/chat_pipeline -run 'TestSearchParallel.*Wiki|TestWikiPagesToSearchResults' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/application/service/chat_pipeline/search_parallel.go internal/application/service/chat_pipeline/search_parallel_test.go internal/container/container.go
git commit -m "feat: run wiki recall in multi-route search"
```

---

### Task 4: Add Frontend Configuration Controls

**Files:**
- Modify: `frontend/src/api/agent/index.ts:72-78`
- Modify: `frontend/src/views/agent/AgentEditorModal.vue:1176-1267`
- Modify: `frontend/src/views/agent/AgentEditorModal.vue:1850-1890`
- Modify: `frontend/src/i18n/locales/zh-CN.ts`
- Modify: `frontend/src/i18n/locales/en-US.ts`

- [ ] **Step 1: Extend the frontend API type**

In `frontend/src/api/agent/index.ts`, after `rerank_threshold?: number;`, add:

```ts
  multi_route_retrieval_enabled?: boolean; // 是否启用 RAG + Wiki + 知识图谱多路召回
  wiki_recall_top_k?: number;              // Wiki 每个知识库召回页数
```

- [ ] **Step 2: Add default form values**

In `frontend/src/views/agent/AgentEditorModal.vue`, inside the default `config` object near retrieval settings, add:

```ts
    multi_route_retrieval_enabled: false,
    wiki_recall_top_k: 5,
```

- [ ] **Step 3: Add controls to the retrieval strategy section**

In the “检索策略” section before the `embedding_top_k` row, add:

```vue
                    <div v-if="!isAgentMode" class="setting-row">
                      <div class="setting-info">
                        <label>{{ $t('agentEditor.multiRoute.enableLabel') }}</label>
                        <p class="desc">{{ $t('agentEditor.multiRoute.enableDesc') }}</p>
                      </div>
                      <div class="setting-control">
                        <t-switch v-model="formData.config.multi_route_retrieval_enabled" />
                      </div>
                    </div>

                    <div v-if="!isAgentMode && formData.config.multi_route_retrieval_enabled" class="setting-row">
                      <div class="setting-info">
                        <label>{{ $t('agentEditor.multiRoute.wikiTopKLabel') }}</label>
                        <p class="desc">{{ $t('agentEditor.multiRoute.wikiTopKDesc') }}</p>
                      </div>
                      <div class="setting-control">
                        <t-input-number v-model="formData.config.wiki_recall_top_k" :min="1" :max="20" theme="column" />
                      </div>
                    </div>
```

This intentionally shows the switch only for quick-answer mode, because smart-reasoning agents already use tool orchestration.

- [ ] **Step 4: Add Chinese i18n strings**

In `frontend/src/i18n/locales/zh-CN.ts`, under `agentEditor`, add:

```ts
    multiRoute: {
      enableLabel: "多路召回",
      enableDesc: "在快速问答模式中并行使用 RAG 分块、知识图谱和 Wiki 页面召回，然后统一重排。",
      wikiTopKLabel: "Wiki 召回页数",
      wikiTopKDesc: "每个 Wiki 知识库最多召回的页面数，推荐 3-8。",
    },
```

- [ ] **Step 5: Add English i18n strings**

In `frontend/src/i18n/locales/en-US.ts`, under `agentEditor`, add:

```ts
    multiRoute: {
      enableLabel: "Multi-route retrieval",
      enableDesc: "In quick-answer mode, recall RAG chunks, knowledge graph evidence, and Wiki pages in parallel before reranking.",
      wikiTopKLabel: "Wiki recall pages",
      wikiTopKDesc: "Maximum Wiki pages to recall per Wiki-enabled knowledge base. Recommended range: 3-8.",
    },
```

- [ ] **Step 6: Run frontend type checking**

Run:

```bash
npm run build
```

Expected: PASS. If the project uses a faster type-check script, run that as well:

```bash
npm run type-check
```

Expected: PASS or “missing script” if the package does not define it.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/api/agent/index.ts frontend/src/views/agent/AgentEditorModal.vue frontend/src/i18n/locales/zh-CN.ts frontend/src/i18n/locales/en-US.ts
git commit -m "feat: expose multi-route retrieval settings"
```

---

### Task 5: Add End-To-End Observability And Guardrails

**Files:**
- Modify: `internal/application/service/chat_pipeline/search_parallel.go`
- Modify: `internal/application/service/chat_pipeline/search_wiki.go`
- Test: `internal/application/service/chat_pipeline/search_wiki_test.go`

- [ ] **Step 1: Add test for top-K normalization**

Append to `internal/application/service/chat_pipeline/search_wiki_test.go`:

```go
func TestNormalizeWikiRecallTopK(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "default", in: 0, want: defaultWikiRecallTopK},
		{name: "negative", in: -1, want: defaultWikiRecallTopK},
		{name: "within range", in: 8, want: 8},
		{name: "clamped", in: 99, want: maxWikiRecallTopK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeWikiRecallTopK(tt.in); got != tt.want {
				t.Fatalf("normalizeWikiRecallTopK(%d)=%d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the failing or passing normalization test**

Run:

```bash
go test ./internal/application/service/chat_pipeline -run TestNormalizeWikiRecallTopK -count=1
```

Expected: PASS if Task 2 already added the normalizer exactly as planned.

- [ ] **Step 3: Add route metrics metadata**

In `search_parallel.go`, include per-route counts in the completion log:

```go
	pipelineInfo(ctx, "SearchParallel", "complete", map[string]interface{}{
		"session_id":     chatManage.SessionID,
		"chunk_results":  len(chunkCM.SearchResult),
		"entity_results": len(entityCM.SearchResult),
		"wiki_results":   len(wikiCM.SearchResult),
		"total_results":  len(chatManage.SearchResult),
		"error_count":    len(errs),
	})
```

In `search_wiki.go`, ensure every result has:

```go
result.Metadata["source_route"] = "wiki"
```

The snippets in Task 2 already set this metadata for chunk-backed and virtual Wiki results.

- [ ] **Step 4: Run the focused tests**

Run:

```bash
go test ./internal/application/service/chat_pipeline -run 'TestSearchParallel.*Wiki|TestWikiPagesToSearchResults|TestNormalizeWikiRecallTopK' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/application/service/chat_pipeline/search_parallel.go internal/application/service/chat_pipeline/search_wiki.go internal/application/service/chat_pipeline/search_wiki_test.go
git commit -m "feat: add multi-route retrieval observability"
```

---

### Task 6: Document The Optimization

**Files:**
- Create: `help/optimization/rag-neo4j-wiki-multiroute-retrieval.md`

- [ ] **Step 1: Create the documentation directory**

Run:

```bash
mkdir -p help/optimization
```

Expected: directory exists.

- [ ] **Step 2: Create the user-facing document**

Create `help/optimization/rag-neo4j-wiki-multiroute-retrieval.md` with:

```markdown
# RAG + Neo4j + Wiki 多路召回改造说明

## 改造原因

当前快速问答模式已经支持 RAG 分块召回与 Neo4j 知识图谱召回并行执行，但 Wiki 主要通过智能体工具链使用，不会在快速问答流水线中自动参与召回。对于架构关系、跨文档总结、实体上下游、概念导航类问题，仅依赖分块相似度和图谱实体匹配可能漏掉 Wiki 中已经沉淀的主题页、实体页和摘要页。

本次改造的目标是把 Wiki 作为第三条可选召回通道加入快速问答流水线，与 RAG 和 Neo4j 并行工作，提升复杂知识问题的召回上限。

## 改造内容

改造后快速问答检索链路如下：

```text
用户问题
  -> QUERY_UNDERSTAND
  -> CHUNK_SEARCH_PARALLEL
       -> RAG 分支：向量 + 关键词
       -> Neo4j 分支：实体 -> 图节点 -> chunk
       -> Wiki 分支：Wiki 页面搜索 -> ChunkRefs/SourceRefs -> 原文证据
  -> 去重
  -> CHUNK_RERANK
  -> CHUNK_MERGE
  -> FILTER_TOP_K
  -> CHAT_COMPLETION_STREAM
```

关键原则：

- `HybridSearch` 仍然只表示向量 + 关键词混合检索。
- 多路召回默认关闭，只有开启 `multi_route_retrieval_enabled` 后才启用 Wiki 召回分支。
- Wiki 命中优先回溯到原文 chunk，避免最终答案只依赖二次生成的 Wiki 内容。
- Neo4j 图谱召回仍沿用现有实体抽取和 `graphRepo.SearchNode` 机制。
- 所有召回结果最终进入现有 rerank、merge 和引用生成流程。

## 使用方法

### 1. 环境准备

如果需要 Neo4j 图谱召回，后端环境需要开启：

```env
NEO4J_ENABLE=true
NEO4J_URI=bolt://neo4j:7687
NEO4J_USERNAME=neo4j
NEO4J_PASSWORD=your-password
```

如果需要在知识库设置页展示知识图谱配置，前端环境通常还需要：

```env
ENABLE_GRAPH_RAG=true
```

### 2. 知识库准备

目标知识库需要按需开启：

- 向量或关键词索引，用于 RAG 分块召回。
- Wiki，用于生成并搜索 Wiki 页面。
- 知识图谱，用于抽取实体和关系并写入 Neo4j。

已有文档如果是在开启 Wiki 或知识图谱前上传的，需要重新处理、重新上传或重建索引，确保 Wiki 页面、Neo4j 图节点和 chunk 关联已经生成。

### 3. 智能体配置

在智能体编辑页：

1. 运行模式选择“快速问答”。
2. 进入“知识库”，绑定目标知识库。
3. 进入“检索策略”。
4. 打开“多路召回”。
5. 设置“Wiki 召回页数”，推荐 3-8。
6. 保存智能体。

### 4. 验证效果

推荐使用关系型和跨文档问题验证：

```text
A 和 B 是什么关系？
系统 X 的上游和下游模块有哪些？
某个产品能力依赖哪些组件？
围绕某个概念有哪些相关实体、文档证据和 Wiki 总结？
```

服务日志中应能看到类似指标：

```text
chunk_results=12
entity_results=5
wiki_results=4
total_results=16
```

如果 `wiki_results=0`，优先检查：

- 智能体是否开启了多路召回。
- 知识库是否启用了 Wiki。
- Wiki 页面是否已经生成。
- 查询词是否能命中 Wiki 页面标题、摘要或内容。

如果 `entity_results=0`，优先检查：

- `NEO4J_ENABLE` 是否为 `true`。
- 知识库是否启用了知识图谱。
- 文档是否已完成图谱抽取。
- 用户问题是否包含可抽取实体。

## 适用场景

适合：

- 企业知识库、产品文档、架构文档、制度文档。
- 关系型、多跳型、跨文档总结型问题。
- 已经稳定生成 Wiki 页面和 Neo4j 图谱的知识库。

不一定适合：

- 小型 FAQ。
- 单文档精确问答。
- Wiki 或图谱质量尚不稳定的知识库。

## 回退方式

关闭智能体的“多路召回”开关即可回退到原快速问答行为。代码层面没有改变 `HybridSearch` 语义，也没有改变默认流水线，因此可以按智能体粒度逐步灰度。
```

- [ ] **Step 3: Commit**

```bash
git add help/optimization/rag-neo4j-wiki-multiroute-retrieval.md
git commit -m "docs: add multi-route retrieval guide"
```

---

### Task 7: Full Verification

**Files:**
- No new files.
- Verify all modified backend, frontend, and documentation files.

- [ ] **Step 1: Run backend focused tests**

Run:

```bash
go test ./internal/types ./internal/application/service/chat_pipeline -count=1
```

Expected: PASS.

- [ ] **Step 2: Run broader backend tests for touched packages**

Run:

```bash
go test ./internal/application/service/... ./internal/types/... -count=1
```

Expected: PASS.

- [ ] **Step 3: Run frontend build**

Run:

```bash
npm run build
```

Expected: PASS.

- [ ] **Step 4: Inspect git diff**

Run:

```bash
git diff --stat
git diff -- internal/application/service/chat_pipeline/search_parallel.go internal/application/service/chat_pipeline/search_wiki.go internal/types/custom_agent.go internal/types/chat_manage.go frontend/src/views/agent/AgentEditorModal.vue help/optimization/rag-neo4j-wiki-multiroute-retrieval.md
```

Expected: changes are limited to the planned files; no unrelated formatting churn.

- [ ] **Step 5: Manual smoke test**

Start the project using the repository’s existing local workflow, then:

1. Open the console.
2. Create or edit a quick-answer agent.
3. Bind a Wiki + Graph + RAG-enabled knowledge base.
4. Open “检索策略”.
5. Enable “多路召回”.
6. Ask: `A 和 B 是什么关系？`
7. Confirm logs include `chunk_results`, `entity_results`, and `wiki_results`.
8. Confirm the final answer still cites retrieved knowledge evidence.

- [ ] **Step 6: Final commit**

```bash
git status --short
git add internal/types/custom_agent.go internal/types/chat_manage.go internal/types/chat_manage_test.go internal/types/embedding.go internal/agent/tools/tool.go internal/application/service/session_qa_helpers.go internal/application/service/chat_pipeline/search_wiki.go internal/application/service/chat_pipeline/search_wiki_test.go internal/application/service/chat_pipeline/search_parallel.go internal/application/service/chat_pipeline/search_parallel_test.go frontend/src/api/agent/index.ts frontend/src/views/agent/AgentEditorModal.vue frontend/src/i18n/locales/zh-CN.ts frontend/src/i18n/locales/en-US.ts help/optimization/rag-neo4j-wiki-multiroute-retrieval.md
git commit -m "feat: add rag graph wiki multi-route retrieval"
```

---

## Self-Review

**Spec coverage:**  
- Current feature branch only: covered in Branch And Upgrade Constraints.  
- RAG + Neo4j + Wiki parallel recall: covered by Tasks 2 and 3.  
- Preserve upstream compatibility: covered by opt-in flags, unchanged `HybridSearch`, and narrow file changes.  
- Usage documentation under `help/optimization`: covered by Task 6.  
- How to use after completion: covered in Task 6 and Task 7 smoke test.

**Placeholder scan:**  
No unresolved placeholder markers or deferred-work wording remain. Every code-changing task includes concrete snippets and commands.

**Type consistency:**  
The fields `MultiRouteRetrievalEnabled`, `WikiRecallTopK`, `multi_route_retrieval_enabled`, and `wiki_recall_top_k` are introduced consistently across backend runtime config, persisted custom agent config, and frontend config.
