package im

import (
	"context"
	"strings"

	"github.com/Tencent/WeKnora/internal/agent/skills"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// ArtifactCollector is the part of the application artifact collector the IM
// service needs. It is an interface so the IM package keeps depending on
// behavior rather than the concrete service, and so Lite deployments can pass
// nil when sandbox file inspection is unavailable.
type ArtifactCollector interface {
	CollectTarget(ctx context.Context, sessionID string) (string, bool)
	CollectWithNotify(
		ctx context.Context,
		sessionID string,
		messageID string,
		tenantID uint64,
		outputDir string,
		notify func(pending int),
	) (types.MessageArtifacts, error)
	BindArtifactsToMessage(ctx context.Context, messageID string, artifacts types.MessageArtifacts)
	// SessionArtifacts returns every artifact already recorded for the
	// session, used to re-send a file the current answer names but did not
	// regenerate.
	SessionArtifacts(ctx context.Context, sessionID string) types.MessageArtifacts
}

// collectIMArtifacts drains the turn's sandbox output into persistent storage
// and attaches it to the assistant message. The web SSE handler does the same
// while the turn's sandbox lease is still held; the event callback runs at that
// point for IM too, which is why collection happens here and not after the
// stream has already been finalized.
func (s *Service) collectIMArtifacts(ctx context.Context, sessionID string, tenantID uint64, assistantMsg *types.Message) {
	if s == nil || s.artifactCollector == nil || assistantMsg == nil || assistantMsg.ID == "" {
		return
	}
	collectCtx := context.WithoutCancel(ctx)
	collectDir, skip := s.artifactCollector.CollectTarget(collectCtx, sessionID)
	if skip {
		return
	}
	if collectDir == "" {
		collectDir = skills.ArtifactOutputDir()
	}
	artifacts, err := s.artifactCollector.CollectWithNotify(
		collectCtx, sessionID, assistantMsg.ID, tenantID, collectDir, nil,
	)
	if err != nil {
		logger.Warnf(collectCtx, "[IM] Artifact collection failed: session=%s message=%s err=%v",
			sessionID, assistantMsg.ID, err)
		return
	}
	if len(artifacts) == 0 {
		return
	}
	assistantMsg.Artifacts = mergeIMArtifacts(assistantMsg.Artifacts, artifacts)
	s.artifactCollector.BindArtifactsToMessage(collectCtx, assistantMsg.ID, artifacts)
	logger.Infof(collectCtx, "[IM] Collected %d artifact(s): session=%s message=%s",
		len(artifacts), sessionID, assistantMsg.ID)
}

func mergeIMArtifacts(base, add types.MessageArtifacts) types.MessageArtifacts {
	if len(add) == 0 {
		return base
	}
	seen := make(map[string]bool, len(base)+len(add))
	for _, artifact := range base {
		seen[artifact.URL+"\x00"+artifact.FileName] = true
	}
	for _, artifact := range add {
		key := artifact.URL + "\x00" + artifact.FileName
		if seen[key] {
			continue
		}
		seen[key] = true
		base = append(base, artifact)
	}
	return base
}

// attachReferencedArtifacts adds session artifacts named by the current
// answer. A later turn can legitimately reference a file it did not generate
// (for example "the docx I made earlier"); without this the IM reply names a
// file that is not actually attached to the message and nothing is sent.
func (s *Service) attachReferencedArtifacts(ctx context.Context, sessionID string, assistantMsg *types.Message, answer string) {
	if s == nil || s.artifactCollector == nil || assistantMsg == nil || strings.TrimSpace(answer) == "" {
		return
	}
	known := s.artifactCollector.SessionArtifacts(ctx, sessionID)
	if len(known) == 0 {
		return
	}
	seen := make(map[string]bool, len(assistantMsg.Artifacts))
	for _, artifact := range assistantMsg.Artifacts {
		seen[artifact.URL] = true
	}
	for _, artifact := range known {
		if artifact.Deleted() || artifact.URL == "" || seen[artifact.URL] {
			continue
		}
		if !strings.Contains(answer, artifact.URL) &&
			(artifact.FileName == "" || !strings.Contains(answer, artifact.FileName)) {
			continue
		}
		seen[artifact.URL] = true
		assistantMsg.Artifacts = append(assistantMsg.Artifacts, artifact)
	}
}
