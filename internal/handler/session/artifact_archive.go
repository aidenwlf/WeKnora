package session

import (
	"bytes"
	"context"
	stderrors "errors"
	"fmt"
	"io"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// ReadArtifactArchive returns the bytes of one chat artifact as a skill bundle
// candidate. It applies exactly the gates artifact download uses — session
// ownership, message lookup, tombstone state and the artifact access resolver —
// so a member can only turn a file they could already download into a skill.
func (h *Handler) ReadArtifactArchive(
	ctx context.Context, sessionID, messageID string, index int,
) (string, []byte, error) {
	if index < 0 {
		return "", nil, fmt.Errorf("invalid artifact index")
	}
	if _, err := h.sessionService.GetSession(ctx, sessionID); err != nil {
		if stderrors.Is(err, errors.ErrSessionNotFound) {
			return "", nil, fmt.Errorf("artifact not accessible")
		}
		return "", nil, err
	}
	msg, err := h.messageService.GetMessage(ctx, sessionID, messageID)
	if err != nil || msg == nil {
		return "", nil, fmt.Errorf("artifact not found")
	}
	if index >= len(msg.Artifacts) {
		return "", nil, fmt.Errorf("artifact not found")
	}
	artifact := msg.Artifacts[index]
	if artifact.Deleted() || artifact.URL == "" {
		return "", nil, fmt.Errorf("artifact not found")
	}

	file, err := access.ResolveMessageArtifact(ctx, msg, index, h.agentShareService, h.resourceCatalog,
		access.MessageKBShareAuthorizer{ShareGuard: h.kbShareService, KBs: h.knowledgebaseService})
	if err != nil {
		return "", nil, fmt.Errorf("artifact not accessible")
	}
	fileService, readCtx, ok := h.resolveArtifactFileService(
		ctx, file.OwnerTenantID, file.Path, file.StorageBackendID, "artifact to skill",
	)
	if !ok {
		return "", nil, fmt.Errorf("artifact storage unavailable")
	}
	reader, err := fileService.GetFile(readCtx, file.Path)
	if err != nil {
		logger.Warnf(ctx, "artifact skill read failed: session=%s message=%s idx=%d err=%v",
			sessionID, messageID, index, err)
		return "", nil, fmt.Errorf("artifact blob missing")
	}
	defer reader.Close()

	// A skill bundle is a zip; the catalog parser rejects anything else, but the
	// size cap is enforced here so an oversized artifact never reaches memory.
	maxBytes := int64(secutils.GetMaxSkillBundleSize())
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(reader, maxBytes+1))
	if err != nil {
		logger.Warnf(ctx, "artifact skill copy failed: session=%s message=%s idx=%d err=%v",
			sessionID, messageID, index, err)
		return "", nil, fmt.Errorf("failed to read artifact")
	}
	if n > maxBytes {
		return "", nil, fmt.Errorf("artifact exceeds the maximum skill bundle size")
	}
	if n == 0 {
		return "", nil, fmt.Errorf("artifact is empty")
	}
	return artifact.FileName, buf.Bytes(), nil
}
