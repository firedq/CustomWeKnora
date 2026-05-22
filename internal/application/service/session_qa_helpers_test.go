package service

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

func TestApplyAgentOverridesToChatManageMultiRouteRetrieval(t *testing.T) {
	svc := &sessionService{}

	cm := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			WikiRecallTopK: 9,
		},
	}
	agent := &types.CustomAgent{
		Config: types.CustomAgentConfig{
			MultiRouteRetrievalEnabled: true,
			WikiRecallTopK:             6,
		},
	}

	svc.applyAgentOverridesToChatManage(context.Background(), agent, cm)

	if !cm.MultiRouteRetrievalEnabled {
		t.Fatal("expected multi-route retrieval to be enabled")
	}
	if cm.WikiRecallTopK != 6 {
		t.Fatalf("expected WikiRecallTopK=6, got %d", cm.WikiRecallTopK)
	}
}

func TestApplyAgentOverridesToChatManageKeepsExistingWikiRecallTopKWhenUnset(t *testing.T) {
	svc := &sessionService{}

	cm := &types.ChatManage{
		PipelineRequest: types.PipelineRequest{
			WikiRecallTopK: 9,
		},
	}
	agent := &types.CustomAgent{
		Config: types.CustomAgentConfig{
			MultiRouteRetrievalEnabled: true,
		},
	}

	svc.applyAgentOverridesToChatManage(context.Background(), agent, cm)

	if cm.WikiRecallTopK != 9 {
		t.Fatalf("expected existing WikiRecallTopK to remain 9, got %d", cm.WikiRecallTopK)
	}
}
