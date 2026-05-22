package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type tenantCaptureWikiService struct {
	interfaces.WikiPageService
	tenants []uint64
}

func (s *tenantCaptureWikiService) SearchPages(ctx context.Context, kbID string, query string, limit int) ([]*types.WikiPage, error) {
	tenantID, _ := types.TenantIDFromContext(ctx)
	s.tenants = append(s.tenants, tenantID)
	return []*types.WikiPage{
		{
			ID:              "page-1",
			TenantID:        tenantID,
			KnowledgeBaseID: kbID,
			Slug:            "concept/rag",
			Title:           "RAG",
			PageType:        types.WikiPageTypeConcept,
		},
	}, nil
}

func TestTruncateForSummary(t *testing.T) {
	tests := []struct {
		name    string
		content string
		maxLen  int
		want    string
	}{
		{
			name:    "short content",
			content: "Hello world",
			maxLen:  50,
			want:    "Hello world",
		},
		{
			name:    "strips heading prefix",
			content: "# My Title\n\nSome content",
			maxLen:  50,
			want:    "My Title",
		},
		{
			name:    "strips h2 prefix",
			content: "## Section\n\nContent here",
			maxLen:  50,
			want:    "Section",
		},
		{
			name:    "truncates long content",
			content: "This is a very long paragraph that should be truncated at some point because it exceeds the maximum length",
			maxLen:  20,
			want:    "This is a very long ..."},
		{
			name:    "takes first paragraph",
			content: "First paragraph.\n\nSecond paragraph.",
			maxLen:  100,
			want:    "First paragraph.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateForSummary(tt.content, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateForSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWikiToolConstants(t *testing.T) {
	// Verify all wiki tool constants are defined and unique
	names := []string{
		ToolWikiReadPage,
		ToolWikiSearch,
	}

	seen := make(map[string]bool)
	for _, name := range names {
		if name == "" {
			t.Error("Wiki tool constant is empty")
		}
		if seen[name] {
			t.Errorf("Duplicate wiki tool constant: %s", name)
		}
		seen[name] = true

		// Verify name length is within OpenAI limit
		if len(name) > maxFunctionNameLength {
			t.Errorf("Wiki tool name too long: %s (%d chars, max %d)", name, len(name), maxFunctionNameLength)
		}
	}
}

func TestWikiToolsInAvailableDefinitions(t *testing.T) {
	defs := AvailableToolDefinitions()
	wikiTools := map[string]bool{
		ToolWikiReadPage: false,
		ToolWikiSearch:   false,
	}

	for _, def := range defs {
		if _, ok := wikiTools[def.Name]; ok {
			wikiTools[def.Name] = true
		}
	}

	for name, found := range wikiTools {
		if !found {
			t.Errorf("Wiki tool %s missing from AvailableToolDefinitions()", name)
		}
	}
}

func TestWikiSearchToolUsesScopeTenant(t *testing.T) {
	wikiService := &tenantCaptureWikiService{}
	tool := NewWikiSearchTool(wikiService, []WikiScope{{KnowledgeBaseID: "kb-1", TenantID: 42}})
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(7))

	result, err := tool.Execute(ctx, json.RawMessage(`{"queries":["rag"],"limit":1}`))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("expected successful result, got %#v", result)
	}
	if len(wikiService.tenants) != 1 || wikiService.tenants[0] != 42 {
		t.Fatalf("expected scope tenant 42, got %#v", wikiService.tenants)
	}
}
