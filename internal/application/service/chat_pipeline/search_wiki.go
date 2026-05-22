package chatpipeline

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

const (
	defaultWikiRecallTopK = 5
	maxWikiRecallTopK     = 20
	maxWikiSearchFetch    = 50
)

// PluginSearchWiki searches generated wiki pages and converts hits to SearchResult.
type PluginSearchWiki struct {
	wikiPageService interfaces.WikiPageService
	chunkRepo       interfaces.ChunkRepository
	knowledgeRepo   interfaces.KnowledgeRepository
}

// NewPluginSearchWiki creates a wiki search plugin without registering it.
func NewPluginSearchWiki(
	wikiPageService interfaces.WikiPageService,
	chunkRepository interfaces.ChunkRepository,
	knowledgeRepository interfaces.KnowledgeRepository,
) *PluginSearchWiki {
	return &PluginSearchWiki{
		wikiPageService: wikiPageService,
		chunkRepo:       chunkRepository,
		knowledgeRepo:   knowledgeRepository,
	}
}

// OnEvent searches wiki pages for the current query and appends converted results.
func (p *PluginSearchWiki) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	if chatManage == nil {
		return next()
	}
	query := strings.TrimSpace(chatManage.RewriteQuery)
	if query == "" {
		query = strings.TrimSpace(chatManage.Query)
	}
	if query == "" || len(chatManage.SearchTargets) == 0 || p.wikiPageService == nil {
		return next()
	}

	topK := normalizeWikiRecallTopK(chatManage.WikiRecallTopK)
	var wikiResults []*types.SearchResult
	for _, target := range chatManage.SearchTargets {
		if target == nil || target.KnowledgeBaseID == "" {
			continue
		}
		targetCtx := ctx
		tenantID := target.TenantID
		if tenantID != 0 {
			targetCtx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
		} else if ctxTenantID, ok := ctx.Value(types.TenantIDContextKey).(uint64); ok {
			tenantID = ctxTenantID
		}

		filterByKnowledge := target.Type == types.SearchTargetTypeKnowledge
		searchLimit := wikiSearchLimitForTarget(topK, filterByKnowledge)
		pages, err := p.wikiPageService.SearchPages(targetCtx, target.KnowledgeBaseID, query, searchLimit)
		if err != nil {
			logger.Errorf(ctx, "Failed to search wiki pages, kb_id: %s, session_id: %s, error: %v",
				target.KnowledgeBaseID, chatManage.SessionID, err)
			return ErrSearch.WithError(err)
		}

		targetKnowledgeIDs := knowledgeIDSet(target.KnowledgeIDs)
		matchedPages := 0
		for _, page := range pages {
			if page == nil {
				continue
			}
			pageResults := p.pageToSearchResults(targetCtx, tenantID, page, targetKnowledgeIDs, filterByKnowledge)
			if len(pageResults) == 0 {
				continue
			}
			wikiResults = append(wikiResults, pageResults...)
			matchedPages++
			if matchedPages >= topK {
				break
			}
		}
	}

	if len(wikiResults) == 0 {
		logger.Infof(ctx, "No wiki search result, session_id: %s", chatManage.SessionID)
		return ErrSearchNothing
	}
	chatManage.SearchResult = appendWikiSearchResults(chatManage.SearchResult, wikiResults)
	return next()
}

func normalizeWikiRecallTopK(topK int) int {
	if topK <= 0 {
		return defaultWikiRecallTopK
	}
	if topK > maxWikiRecallTopK {
		return maxWikiRecallTopK
	}
	return topK
}

func wikiSearchLimitForTarget(topK int, filterByKnowledge bool) int {
	if !filterByKnowledge {
		return topK
	}
	limit := topK * 4
	if limit < defaultWikiRecallTopK {
		limit = defaultWikiRecallTopK
	}
	if limit > maxWikiSearchFetch {
		limit = maxWikiSearchFetch
	}
	return limit
}

func (p *PluginSearchWiki) pageToSearchResults(
	ctx context.Context,
	tenantID uint64,
	page *types.WikiPage,
	targetKnowledgeIDs map[string]struct{},
	filterByKnowledge bool,
) []*types.SearchResult {
	hasChunkRefs := len(page.ChunkRefs) > 0
	if len(page.ChunkRefs) == 0 || p.chunkRepo == nil || p.knowledgeRepo == nil {
		if filterByKnowledge && !wikiPageMatchesKnowledgeIDs(page, targetKnowledgeIDs) {
			return nil
		}
		return []*types.SearchResult{virtualWikiSearchResult(page, targetKnowledgeIDs, filterByKnowledge)}
	}

	chunkIDs := stringArrayToSlice(page.ChunkRefs)
	chunks, err := p.chunkRepo.ListChunksByID(ctx, tenantID, chunkIDs)
	if err != nil {
		logger.Warnf(ctx, "Failed to fetch wiki source chunks, wiki_page_id: %s, error: %v", page.ID, err)
		if filterByKnowledge && hasChunkRefs {
			return nil
		}
		if filterByKnowledge && !wikiPageMatchesKnowledgeIDs(page, targetKnowledgeIDs) {
			return nil
		}
		return []*types.SearchResult{virtualWikiSearchResult(page, targetKnowledgeIDs, filterByKnowledge)}
	}
	if filterByKnowledge {
		chunks = filterChunksByKnowledgeIDs(chunks, targetKnowledgeIDs)
	}
	if len(chunks) == 0 {
		if filterByKnowledge && hasChunkRefs {
			return nil
		}
		if filterByKnowledge && !wikiPageMatchesKnowledgeIDs(page, targetKnowledgeIDs) {
			return nil
		}
		return []*types.SearchResult{virtualWikiSearchResult(page, targetKnowledgeIDs, filterByKnowledge)}
	}

	knowledgeIDs := uniqueChunkKnowledgeIDs(chunks)
	knowledges, err := p.knowledgeRepo.GetKnowledgeBatch(ctx, tenantID, knowledgeIDs)
	if err != nil {
		logger.Warnf(ctx, "Failed to fetch wiki source knowledge, wiki_page_id: %s, error: %v", page.ID, err)
		if filterByKnowledge && hasChunkRefs {
			return nil
		}
		if filterByKnowledge && !wikiPageMatchesKnowledgeIDs(page, targetKnowledgeIDs) {
			return nil
		}
		return []*types.SearchResult{virtualWikiSearchResult(page, targetKnowledgeIDs, filterByKnowledge)}
	}

	knowledgeMap := make(map[string]*types.Knowledge, len(knowledges))
	for _, knowledge := range knowledges {
		if knowledge != nil {
			knowledgeMap[knowledge.ID] = knowledge
		}
	}

	results := make([]*types.SearchResult, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		results = append(results, wikiChunkSearchResult(chunk, knowledgeMap[chunk.KnowledgeID], page))
	}
	if len(results) == 0 {
		if filterByKnowledge && hasChunkRefs {
			return nil
		}
		if filterByKnowledge && !wikiPageMatchesKnowledgeIDs(page, targetKnowledgeIDs) {
			return nil
		}
		return []*types.SearchResult{virtualWikiSearchResult(page, targetKnowledgeIDs, filterByKnowledge)}
	}
	return results
}

func wikiChunkSearchResult(chunk *types.Chunk, knowledge *types.Knowledge, page *types.WikiPage) *types.SearchResult {
	result := &types.SearchResult{
		ID:            chunk.ID,
		Content:       chunk.Content,
		KnowledgeID:   chunk.KnowledgeID,
		ChunkIndex:    chunk.ChunkIndex,
		StartAt:       chunk.StartAt,
		EndAt:         chunk.EndAt,
		Seq:           chunk.ChunkIndex,
		Score:         1.0,
		MatchType:     types.MatchTypeWikiPage,
		Metadata:      wikiMetadata(page, nil),
		ChunkType:     string(chunk.ChunkType),
		ParentChunkID: chunk.ParentChunkID,
		ImageInfo:     chunk.ImageInfo,
		ChunkMetadata: chunk.Metadata,
		KnowledgeBaseID: firstNonEmpty(
			chunk.KnowledgeBaseID,
			page.KnowledgeBaseID,
		),
	}
	if knowledge != nil {
		result.KnowledgeTitle = knowledge.Title
		result.KnowledgeFilename = knowledge.FileName
		result.KnowledgeSource = knowledge.Source
		result.KnowledgeChannel = knowledge.Channel
		result.KnowledgeDescription = knowledge.Description
		result.Metadata = wikiMetadata(page, knowledge.GetMetadata())
		if result.KnowledgeBaseID == "" {
			result.KnowledgeBaseID = knowledge.KnowledgeBaseID
		}
	}
	return result
}

func virtualWikiSearchResult(
	page *types.WikiPage,
	targetKnowledgeIDs map[string]struct{},
	filterByKnowledge bool,
) *types.SearchResult {
	knowledgeID := firstSourceRefKnowledgeID(page.SourceRefs)
	if filterByKnowledge {
		knowledgeID = firstMatchingSourceRefKnowledgeID(page.SourceRefs, targetKnowledgeIDs)
	}
	return &types.SearchResult{
		ID:              "wiki-" + page.ID,
		Content:         wikiVirtualContent(page),
		KnowledgeID:     knowledgeID,
		KnowledgeTitle:  page.Title,
		Score:           1.0,
		MatchType:       types.MatchTypeWikiPage,
		Metadata:        wikiMetadata(page, nil),
		ChunkType:       types.ChunkTypeWikiPage,
		KnowledgeBaseID: page.KnowledgeBaseID,
	}
}

func wikiMetadata(page *types.WikiPage, base map[string]string) map[string]string {
	metadata := make(map[string]string, len(base)+6)
	for k, v := range base {
		metadata[k] = v
	}
	putWikiMetadata(metadata, "retrieval_source", "wiki")
	putWikiMetadata(metadata, "wiki_page_id", page.ID)
	putWikiMetadata(metadata, "wiki_page_slug", page.Slug)
	putWikiMetadata(metadata, "wiki_page_title", page.Title)
	putWikiMetadata(metadata, "wiki_page_type", page.PageType)
	if page.Summary != "" {
		putWikiMetadata(metadata, "wiki_page_summary", page.Summary)
	}
	return metadata
}

func putWikiMetadata(metadata map[string]string, key string, value string) {
	if existing, ok := metadata[key]; ok && existing != "" && existing != value {
		metadata["source_"+key] = existing
	}
	metadata[key] = value
}

func wikiVirtualContent(page *types.WikiPage) string {
	parts := make([]string, 0, 3)
	if page.Title != "" {
		parts = append(parts, fmt.Sprintf("# %s", page.Title))
	}
	if page.Summary != "" {
		parts = append(parts, page.Summary)
	}
	if page.Content != "" {
		parts = append(parts, page.Content)
	}
	return strings.Join(parts, "\n\n")
}

func wikiPageMatchesKnowledgeIDs(page *types.WikiPage, targetKnowledgeIDs map[string]struct{}) bool {
	if len(targetKnowledgeIDs) == 0 {
		return false
	}
	for _, sourceRef := range page.SourceRefs {
		if _, ok := targetKnowledgeIDs[parseSourceRefKnowledgeID(sourceRef)]; ok {
			return true
		}
	}
	return false
}

func knowledgeIDSet(ids []string) map[string]struct{} {
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			set[id] = struct{}{}
		}
	}
	return set
}

func firstSourceRefKnowledgeID(sourceRefs types.StringArray) string {
	for _, sourceRef := range sourceRefs {
		if knowledgeID := parseSourceRefKnowledgeID(sourceRef); knowledgeID != "" {
			return knowledgeID
		}
	}
	return ""
}

func firstMatchingSourceRefKnowledgeID(sourceRefs types.StringArray, targetKnowledgeIDs map[string]struct{}) string {
	for _, sourceRef := range sourceRefs {
		knowledgeID := parseSourceRefKnowledgeID(sourceRef)
		if _, ok := targetKnowledgeIDs[knowledgeID]; ok {
			return knowledgeID
		}
	}
	return ""
}

func parseSourceRefKnowledgeID(sourceRef string) string {
	if sourceRef == "" {
		return ""
	}
	knowledgeID, _, _ := strings.Cut(sourceRef, "|")
	return strings.TrimSpace(knowledgeID)
}

func stringArrayToSlice(values types.StringArray) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func uniqueChunkKnowledgeIDs(chunks []*types.Chunk) []string {
	seen := make(map[string]struct{}, len(chunks))
	ids := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil || chunk.KnowledgeID == "" {
			continue
		}
		if _, ok := seen[chunk.KnowledgeID]; ok {
			continue
		}
		seen[chunk.KnowledgeID] = struct{}{}
		ids = append(ids, chunk.KnowledgeID)
	}
	return ids
}

func filterChunksByKnowledgeIDs(chunks []*types.Chunk, targetKnowledgeIDs map[string]struct{}) []*types.Chunk {
	filtered := make([]*types.Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		if _, ok := targetKnowledgeIDs[chunk.KnowledgeID]; ok {
			filtered = append(filtered, chunk)
		}
	}
	return filtered
}

func appendWikiSearchResults(existingResults []*types.SearchResult, wikiResults []*types.SearchResult) []*types.SearchResult {
	resultByID := make(map[string]*types.SearchResult, len(existingResults))
	for _, result := range existingResults {
		if result == nil || result.ID == "" {
			continue
		}
		if _, exists := resultByID[result.ID]; !exists {
			resultByID[result.ID] = result
		}
	}

	merged := append([]*types.SearchResult(nil), existingResults...)
	for _, wikiResult := range wikiResults {
		if wikiResult == nil {
			continue
		}
		if existing := resultByID[wikiResult.ID]; existing != nil {
			mergeWikiResultIntoExisting(existing, wikiResult)
			continue
		}
		merged = append(merged, wikiResult)
		if wikiResult.ID != "" {
			resultByID[wikiResult.ID] = wikiResult
		}
	}
	return removeDuplicateResults(merged)
}

func mergeWikiResultIntoExisting(existing *types.SearchResult, wikiResult *types.SearchResult) {
	if existing.Metadata == nil {
		existing.Metadata = make(map[string]string)
	}
	if existing.MatchType != types.MatchTypeWikiPage {
		existing.Metadata["source_match_type"] = fmt.Sprintf("%d", existing.MatchType)
	}
	existing.MatchType = types.MatchTypeWikiPage
	for key, value := range wikiResult.Metadata {
		putWikiMetadata(existing.Metadata, key, value)
	}
	if existing.KnowledgeBaseID == "" {
		existing.KnowledgeBaseID = wikiResult.KnowledgeBaseID
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
