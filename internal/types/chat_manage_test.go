package types

import "testing"

func TestChatManageCloneCopiesMultiRouteRetrievalConfigAndSearchTargetTenant(t *testing.T) {
	original := &ChatManage{
		PipelineRequest: PipelineRequest{
			MultiRouteRetrievalEnabled: true,
			WikiRecallTopK:             7,
			SearchTargets: SearchTargets{
				{
					Type:            SearchTargetTypeKnowledgeBase,
					KnowledgeBaseID: "kb-1",
					TenantID:        42,
				},
				{
					Type:            SearchTargetTypeKnowledge,
					KnowledgeBaseID: "kb-2",
					TenantID:        43,
					KnowledgeIDs:    []string{"knowledge-1", "knowledge-2"},
				},
			},
		},
	}

	clone := original.Clone()

	if !clone.MultiRouteRetrievalEnabled {
		t.Fatal("expected MultiRouteRetrievalEnabled to be copied")
	}
	if clone.WikiRecallTopK != 7 {
		t.Fatalf("expected WikiRecallTopK=7, got %d", clone.WikiRecallTopK)
	}
	if len(clone.SearchTargets) != len(original.SearchTargets) {
		t.Fatalf("expected %d search targets, got %d", len(original.SearchTargets), len(clone.SearchTargets))
	}
	if clone.SearchTargets[0].TenantID != 42 {
		t.Fatalf("expected first target TenantID=42, got %d", clone.SearchTargets[0].TenantID)
	}
	if clone.SearchTargets[1].TenantID != 43 {
		t.Fatalf("expected second target TenantID=43, got %d", clone.SearchTargets[1].TenantID)
	}

	clone.SearchTargets[0].TenantID = 100
	clone.SearchTargets[1].KnowledgeIDs[0] = "changed"

	if original.SearchTargets[0].TenantID != 42 {
		t.Fatalf("expected original first target TenantID to remain 42, got %d", original.SearchTargets[0].TenantID)
	}
	if original.SearchTargets[1].KnowledgeIDs[0] != "knowledge-1" {
		t.Fatalf("expected original KnowledgeIDs to be unchanged, got %q", original.SearchTargets[1].KnowledgeIDs[0])
	}
}
