package im

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
)

type fakeArtifactCollector struct {
	dir       string
	skip      bool
	artifacts types.MessageArtifacts
	messageID string
}

func (f *fakeArtifactCollector) CollectTarget(context.Context, string) (string, bool) {
	return f.dir, f.skip
}

func (f *fakeArtifactCollector) CollectWithNotify(context.Context, string, string, uint64, string, func(int)) (types.MessageArtifacts, error) {
	return f.artifacts, nil
}

func (f *fakeArtifactCollector) BindArtifactsToMessage(_ context.Context, messageID string, _ types.MessageArtifacts) {
	f.messageID = messageID
}

func TestCollectIMArtifactsMergesAndBinds(t *testing.T) {
	collector := &fakeArtifactCollector{
		artifacts: types.MessageArtifacts{{URL: "local://tenant/1/report.docx", FileName: "report.docx"}},
	}
	svc := &Service{artifactCollector: collector}
	msg := &types.Message{
		ID: "message-1",
		Artifacts: types.MessageArtifacts{
			{URL: "local://tenant/1/report.docx", FileName: "report.docx"},
			{URL: "local://tenant/1/child.pptx", FileName: "child.pptx"},
		},
	}

	svc.collectIMArtifacts(context.Background(), "session-1", 1, msg)

	if len(msg.Artifacts) != 2 {
		t.Fatalf("artifacts = %#v, want 2 unique entries", msg.Artifacts)
	}
	if collector.messageID != "message-1" {
		t.Fatalf("bound message = %q", collector.messageID)
	}
}

func TestCollectIMArtifactsSkipsWhenTargetUnavailable(t *testing.T) {
	collector := &fakeArtifactCollector{skip: true}
	svc := &Service{artifactCollector: collector}
	msg := &types.Message{ID: "message-1"}
	svc.collectIMArtifacts(context.Background(), "session-1", 1, msg)
	if len(msg.Artifacts) != 0 {
		t.Fatalf("artifacts = %#v, want none", msg.Artifacts)
	}
}
