-- Approval requests for installing a chat-generated skill bundle from a
-- member who cannot write the workspace skill catalog themselves.
CREATE TABLE IF NOT EXISTS tenant_skill_approval_requests (
    id             BIGSERIAL PRIMARY KEY,
    tenant_id      BIGINT       NOT NULL,
    requester_id   VARCHAR(64)  NOT NULL,
    session_id     VARCHAR(36)  NOT NULL,
    message_id     VARCHAR(36)  NOT NULL,
    artifact_index INTEGER      NOT NULL,
    file_name      VARCHAR(255) NOT NULL DEFAULT '',
    config_ids     TEXT         NOT NULL DEFAULT '[]',
    status         VARCHAR(32)  NOT NULL DEFAULT 'pending',
    approver_id    VARCHAR(64)  NOT NULL DEFAULT '',
    reason         TEXT         NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_skill_approval_tenant_status
    ON tenant_skill_approval_requests (tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_skill_approval_requester
    ON tenant_skill_approval_requests (tenant_id, requester_id);
