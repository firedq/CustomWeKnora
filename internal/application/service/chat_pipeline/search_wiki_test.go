package chatpipeline

import (
	"context"
	"reflect"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type fakeWikiPageService struct {
	interfaces.WikiPageService
	pagesByKB map[string][]*types.WikiPage
	limits    []int
	tenants   []uint64
}

func (f *fakeWikiPageService) SearchPages(ctx context.Context, kbID string, query string, limit int) ([]*types.WikiPage, error) {
	f.limits = append(f.limits, limit)
	tenantID, _ := types.TenantIDFromContext(ctx)
	f.tenants = append(f.tenants, tenantID)
	return f.pagesByKB[kbID], nil
}

type fakeChunkRepo struct {
	interfaces.ChunkRepository
	chunksByID map[string]*types.Chunk
	tenants    []uint64
}

func (f *fakeChunkRepo) ListChunksByID(ctx context.Context, tenantID uint64, ids []string) ([]*types.Chunk, error) {
	f.tenants = append(f.tenants, tenantID)
	var chunks []*types.Chunk
	for _, id := range ids {
		if chunk := f.chunksByID[id]; chunk != nil {
			chunks = append(chunks, chunk)
		}
	}
	return chunks, nil
}

type fakeKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	knowledgeByID map[string]*types.Knowledge
	tenants       []uint64
}

func (f *fakeKnowledgeRepo) GetKnowledgeBatch(ctx context.Context, tenantID uint64, ids []string) ([]*types.Knowledge, error) {
	f.tenants = append(f.tenants, tenantID)
	var knowledges []*types.Knowledge
	for _, id := range ids {
		if knowledge := f.knowledgeByID[id]; knowledge != nil {
			knowledges = append(knowledges, knowledge)
		}
	}
	return knowledges, nil
}

func TestPluginSearchWikiChunkRefsBecomeChunkSearchResults(t *testing.T) {
	wikiService := &fakeWikiPageService{
		pagesByKB: map[string][]*types.WikiPage{
			"kb-1": {
				{
					ID:              "page-1",
					KnowledgeBaseID: "kb-1",
					Slug:            "concept/rag",
					Title:           "RAG",
					PageType:        types.WikiPageTypeConcept,
					Summary:         "retrieval summary",
					SourceRefs:      types.StringArray{"knowledge-1|doc.md"},
					ChunkRefs:       types.StringArray{"chunk-1"},
				},
			},
		},
	}
	chunkRepo := &fakeChunkRepo{
		chunksByID: map[string]*types.Chunk{
			"chunk-1": {
				ID:              "chunk-1",
				Content:         "original chunk content",
				KnowledgeID:     "knowledge-1",
				KnowledgeBaseID: "kb-1",
				ChunkIndex:      3,
				ChunkType:       types.ChunkTypeText,
			},
		},
	}
	knowledgeRepo := &fakeKnowledgeRepo{
		knowledgeByID: map[string]*types.Knowledge{
			"knowledge-1": {
				ID:              "knowledge-1",
				KnowledgeBaseID: "kb-1",
				Title:           "Original Doc",
				FileName:        "doc.md",
				Metadata:        types.JSON(`{"retrieval_source":"document","wiki_page_id":"source-page"}`),
			},
		},
	}
	plugin := NewPluginSearchWiki(wikiService, chunkRepo, knowledgeRepo)

	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query: "rag",
			SearchTargets: types.SearchTargets{
				{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 42},
			},
			WikiRecallTopK: 3,
		},
	}

	err := plugin.OnEvent(context.Background(), types.CHUNK_SEARCH, chatManage, func() *PluginError { return nil })
	if err != nil {
		t.Fatalf("OnEvent returned error: %v", err)
	}
	if len(chatManage.SearchResult) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(chatManage.SearchResult))
	}
	result := chatManage.SearchResult[0]
	if result.ID != "chunk-1" || result.Content != "original chunk content" {
		t.Fatalf("expected original chunk result, got id=%q content=%q", result.ID, result.Content)
	}
	if result.MatchType != types.MatchTypeWikiPage {
		t.Fatalf("expected wiki match type, got %v", result.MatchType)
	}
	if result.Metadata["retrieval_source"] != "wiki" ||
		result.Metadata["source_retrieval_source"] != "document" ||
		result.Metadata["wiki_page_id"] != "page-1" ||
		result.Metadata["source_wiki_page_id"] != "source-page" ||
		result.Metadata["wiki_page_slug"] != "concept/rag" ||
		result.Metadata["wiki_page_title"] != "RAG" ||
		result.Metadata["wiki_page_type"] != types.WikiPageTypeConcept ||
		result.Metadata["wiki_page_summary"] != "retrieval summary" {
		t.Fatalf("unexpected metadata: %#v", result.Metadata)
	}
	if !reflect.DeepEqual(wikiService.tenants, []uint64{42}) {
		t.Fatalf("expected wiki search tenant 42, got %#v", wikiService.tenants)
	}
	if !reflect.DeepEqual(chunkRepo.tenants, []uint64{42}) {
		t.Fatalf("expected chunk repo tenant 42, got %#v", chunkRepo.tenants)
	}
	if !reflect.DeepEqual(knowledgeRepo.tenants, []uint64{42}) {
		t.Fatalf("expected knowledge repo tenant 42, got %#v", knowledgeRepo.tenants)
	}
}

func TestPluginSearchWikiNoChunkRefsCreatesVirtualWikiResult(t *testing.T) {
	plugin := NewPluginSearchWiki(&fakeWikiPageService{
		pagesByKB: map[string][]*types.WikiPage{
			"kb-1": {
				{
					ID:              "page-1",
					KnowledgeBaseID: "kb-1",
					Slug:            "summary/doc",
					Title:           "Doc Summary",
					PageType:        types.WikiPageTypeSummary,
					Summary:         "short summary",
					Content:         "long wiki content",
					SourceRefs:      types.StringArray{"knowledge-1|doc.md"},
				},
			},
		},
	}, &fakeChunkRepo{}, &fakeKnowledgeRepo{})

	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query: "doc",
			SearchTargets: types.SearchTargets{
				{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 42},
			},
		},
	}

	err := plugin.OnEvent(context.Background(), types.CHUNK_SEARCH, chatManage, func() *PluginError { return nil })
	if err != nil {
		t.Fatalf("OnEvent returned error: %v", err)
	}
	if len(chatManage.SearchResult) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(chatManage.SearchResult))
	}
	result := chatManage.SearchResult[0]
	if result.ID != "wiki-page-1" {
		t.Fatalf("expected virtual wiki id, got %q", result.ID)
	}
	if result.ChunkType != types.ChunkTypeWikiPage {
		t.Fatalf("expected wiki chunk type, got %q", result.ChunkType)
	}
	if result.KnowledgeID != "knowledge-1" {
		t.Fatalf("expected parsed knowledge id, got %q", result.KnowledgeID)
	}
	if result.Content == "" || result.MatchType != types.MatchTypeWikiPage {
		t.Fatalf("unexpected virtual result: %#v", result)
	}
}

func TestPluginSearchWikiKnowledgeTargetFiltersBySourceRefs(t *testing.T) {
	plugin := NewPluginSearchWiki(&fakeWikiPageService{
		pagesByKB: map[string][]*types.WikiPage{
			"kb-1": {
				{
					ID:              "page-1",
					KnowledgeBaseID: "kb-1",
					Slug:            "target",
					Title:           "Target",
					PageType:        types.WikiPageTypeConcept,
					SourceRefs:      types.StringArray{"knowledge-1|target.md"},
				},
				{
					ID:              "page-2",
					KnowledgeBaseID: "kb-1",
					Slug:            "other",
					Title:           "Other",
					PageType:        types.WikiPageTypeConcept,
					SourceRefs:      types.StringArray{"knowledge-2|other.md"},
				},
			},
		},
	}, &fakeChunkRepo{}, &fakeKnowledgeRepo{})

	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query: "topic",
			SearchTargets: types.SearchTargets{
				{
					Type:            types.SearchTargetTypeKnowledge,
					KnowledgeBaseID: "kb-1",
					TenantID:        42,
					KnowledgeIDs:    []string{"knowledge-1"},
				},
			},
		},
	}

	err := plugin.OnEvent(context.Background(), types.CHUNK_SEARCH, chatManage, func() *PluginError { return nil })
	if err != nil {
		t.Fatalf("OnEvent returned error: %v", err)
	}
	if len(chatManage.SearchResult) != 1 {
		t.Fatalf("expected 1 search result, got %d", len(chatManage.SearchResult))
	}
	if chatManage.SearchResult[0].Metadata["wiki_page_id"] != "page-1" {
		t.Fatalf("expected target page only, got %#v", chatManage.SearchResult[0].Metadata)
	}
}

func TestPluginSearchWikiKnowledgeTargetFallsBackToChunkKnowledgeID(t *testing.T) {
	plugin := NewPluginSearchWiki(&fakeWikiPageService{
		pagesByKB: map[string][]*types.WikiPage{
			"kb-1": {
				{
					ID:              "page-1",
					KnowledgeBaseID: "kb-1",
					Slug:            "chunk-backed",
					Title:           "Chunk Backed",
					PageType:        types.WikiPageTypeConcept,
					ChunkRefs:       types.StringArray{"chunk-1", "chunk-2"},
				},
			},
		},
	}, &fakeChunkRepo{
		chunksByID: map[string]*types.Chunk{
			"chunk-1": {
				ID:              "chunk-1",
				Content:         "target chunk",
				KnowledgeID:     "knowledge-1",
				KnowledgeBaseID: "kb-1",
				ChunkType:       types.ChunkTypeText,
			},
			"chunk-2": {
				ID:              "chunk-2",
				Content:         "other chunk",
				KnowledgeID:     "knowledge-2",
				KnowledgeBaseID: "kb-1",
				ChunkType:       types.ChunkTypeText,
			},
		},
	}, &fakeKnowledgeRepo{
		knowledgeByID: map[string]*types.Knowledge{
			"knowledge-1": {ID: "knowledge-1", KnowledgeBaseID: "kb-1", Title: "Target Doc"},
		},
	})

	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query: "topic",
			SearchTargets: types.SearchTargets{
				{
					Type:            types.SearchTargetTypeKnowledge,
					KnowledgeBaseID: "kb-1",
					TenantID:        42,
					KnowledgeIDs:    []string{" knowledge-1 "},
				},
			},
		},
	}

	err := plugin.OnEvent(context.Background(), types.CHUNK_SEARCH, chatManage, func() *PluginError { return nil })
	if err != nil {
		t.Fatalf("OnEvent returned error: %v", err)
	}
	if len(chatManage.SearchResult) != 1 {
		t.Fatalf("expected 1 filtered chunk result, got %d", len(chatManage.SearchResult))
	}
	if chatManage.SearchResult[0].ID != "chunk-1" {
		t.Fatalf("expected only target chunk, got %#v", chatManage.SearchResult)
	}
}

func TestPluginSearchWikiKnowledgeTargetWithChunkRefsFailsClosedWhenChunksDoNotMatch(t *testing.T) {
	plugin := NewPluginSearchWiki(&fakeWikiPageService{
		pagesByKB: map[string][]*types.WikiPage{
			"kb-1": {
				{
					ID:              "page-1",
					KnowledgeBaseID: "kb-1",
					Slug:            "mixed-page",
					Title:           "Mixed Page",
					PageType:        types.WikiPageTypeConcept,
					SourceRefs:      types.StringArray{"knowledge-1|target.md"},
					ChunkRefs:       types.StringArray{"chunk-2"},
				},
			},
		},
	}, &fakeChunkRepo{
		chunksByID: map[string]*types.Chunk{
			"chunk-2": {
				ID:              "chunk-2",
				Content:         "other chunk",
				KnowledgeID:     "knowledge-2",
				KnowledgeBaseID: "kb-1",
				ChunkType:       types.ChunkTypeText,
			},
		},
	}, &fakeKnowledgeRepo{})

	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query: "topic",
			SearchTargets: types.SearchTargets{
				{
					Type:            types.SearchTargetTypeKnowledge,
					KnowledgeBaseID: "kb-1",
					TenantID:        42,
					KnowledgeIDs:    []string{"knowledge-1"},
				},
			},
		},
	}

	err := plugin.OnEvent(context.Background(), types.CHUNK_SEARCH, chatManage, func() *PluginError { return nil })
	if err != ErrSearchNothing {
		t.Fatalf("expected ErrSearchNothing, got %#v", err)
	}
	if len(chatManage.SearchResult) != 0 {
		t.Fatalf("expected no results, got %#v", chatManage.SearchResult)
	}
}

func TestAppendWikiSearchResultsMergesDuplicateChunkMetadata(t *testing.T) {
	existing := []*types.SearchResult{
		{
			ID:        "chunk-1",
			Content:   "original chunk",
			MatchType: types.MatchTypeEmbedding,
			Metadata:  map[string]string{"retrieval_source": "vector"},
		},
	}
	wikiResults := []*types.SearchResult{
		{
			ID:        "chunk-1",
			Content:   "original chunk",
			MatchType: types.MatchTypeWikiPage,
			Metadata: map[string]string{
				"retrieval_source": "wiki",
				"wiki_page_id":     "page-1",
				"wiki_page_title":  "RAG",
			},
		},
	}

	merged := appendWikiSearchResults(existing, wikiResults)

	if len(merged) != 1 {
		t.Fatalf("expected 1 merged result, got %d", len(merged))
	}
	result := merged[0]
	if result.MatchType != types.MatchTypeWikiPage {
		t.Fatalf("expected wiki match type after merge, got %v", result.MatchType)
	}
	if result.Metadata["retrieval_source"] != "wiki" ||
		result.Metadata["source_retrieval_source"] != "vector" ||
		result.Metadata["source_match_type"] == "" ||
		result.Metadata["wiki_page_id"] != "page-1" {
		t.Fatalf("unexpected merged metadata: %#v", result.Metadata)
	}
}

func TestAppendWikiSearchResultsMergesIntoFirstDuplicateExistingResult(t *testing.T) {
	existing := []*types.SearchResult{
		{
			ID:        "chunk-1",
			Content:   "original chunk",
			MatchType: types.MatchTypeEmbedding,
			Metadata:  map[string]string{"retrieval_source": "vector"},
		},
		{
			ID:        "chunk-1",
			Content:   "original chunk",
			MatchType: types.MatchTypeGraph,
			Metadata:  map[string]string{"retrieval_source": "graph"},
		},
	}
	wikiResults := []*types.SearchResult{
		{
			ID:        "chunk-1",
			Content:   "original chunk",
			MatchType: types.MatchTypeWikiPage,
			Metadata: map[string]string{
				"retrieval_source": "wiki",
				"wiki_page_id":     "page-1",
			},
		},
	}

	merged := appendWikiSearchResults(existing, wikiResults)

	if len(merged) != 1 {
		t.Fatalf("expected 1 merged result, got %d", len(merged))
	}
	if merged[0].MatchType != types.MatchTypeWikiPage {
		t.Fatalf("expected wiki match type on preserved first result, got %v", merged[0].MatchType)
	}
	if merged[0].Metadata["wiki_page_id"] != "page-1" ||
		merged[0].Metadata["source_retrieval_source"] != "vector" {
		t.Fatalf("expected wiki metadata merged into first existing result, got %#v", merged[0].Metadata)
	}
}

func TestPluginSearchWikiDefaultTopK(t *testing.T) {
	wikiService := &fakeWikiPageService{
		pagesByKB: map[string][]*types.WikiPage{
			"kb-1": {
				{ID: "page-1", KnowledgeBaseID: "kb-1", Slug: "page", Title: "Page", PageType: types.WikiPageTypeConcept},
			},
		},
	}
	plugin := NewPluginSearchWiki(wikiService, &fakeChunkRepo{}, &fakeKnowledgeRepo{})
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:          "topic",
			WikiRecallTopK: 0,
			SearchTargets: types.SearchTargets{
				{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: "kb-1", TenantID: 42},
			},
		},
	}

	err := plugin.OnEvent(context.Background(), types.CHUNK_SEARCH, chatManage, func() *PluginError { return nil })
	if err != nil {
		t.Fatalf("OnEvent returned error: %v", err)
	}
	if !reflect.DeepEqual(wikiService.limits, []int{5}) {
		t.Fatalf("expected default limit 5, got %#v", wikiService.limits)
	}
}

func TestPluginSearchWikiKnowledgeTargetOverfetchesBeforeFiltering(t *testing.T) {
	wikiService := &fakeWikiPageService{
		pagesByKB: map[string][]*types.WikiPage{
			"kb-1": {
				{ID: "page-1", KnowledgeBaseID: "kb-1", Slug: "other-1", Title: "Other 1", PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{"knowledge-2|other.md"}},
				{ID: "page-2", KnowledgeBaseID: "kb-1", Slug: "other-2", Title: "Other 2", PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{"knowledge-3|other.md"}},
				{ID: "page-3", KnowledgeBaseID: "kb-1", Slug: "target", Title: "Target", PageType: types.WikiPageTypeConcept, SourceRefs: types.StringArray{"knowledge-1|target.md"}},
			},
		},
	}
	plugin := NewPluginSearchWiki(wikiService, &fakeChunkRepo{}, &fakeKnowledgeRepo{})
	chatManage := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			Query:          "topic",
			WikiRecallTopK: 1,
			SearchTargets: types.SearchTargets{
				{
					Type:            types.SearchTargetTypeKnowledge,
					KnowledgeBaseID: "kb-1",
					TenantID:        42,
					KnowledgeIDs:    []string{"knowledge-1"},
				},
			},
		},
	}

	err := plugin.OnEvent(context.Background(), types.CHUNK_SEARCH, chatManage, func() *PluginError { return nil })
	if err != nil {
		t.Fatalf("OnEvent returned error: %v", err)
	}
	if !reflect.DeepEqual(wikiService.limits, []int{5}) {
		t.Fatalf("expected partial target overfetch limit 5, got %#v", wikiService.limits)
	}
	if len(chatManage.SearchResult) != 1 {
		t.Fatalf("expected 1 filtered wiki result, got %d", len(chatManage.SearchResult))
	}
	if chatManage.SearchResult[0].Metadata["wiki_page_id"] != "page-3" {
		t.Fatalf("expected target page after overfetch/filter, got %#v", chatManage.SearchResult[0].Metadata)
	}
}
