-- Collaborative spreadsheets (Univer). Cell data lives in the collaboration
-- server's own schema; this table only records workspace ownership so the API
-- can list sheets and the collaboration server's ACL bridge can map a unit
-- back to its workspace.
CREATE TABLE IF NOT EXISTS univer_sheets (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    name VARCHAR(200) NOT NULL,
    unit_id VARCHAR(128) NOT NULL,
    creator_id VARCHAR(128) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_univer_sheets_unit_id ON univer_sheets(unit_id);
CREATE INDEX IF NOT EXISTS idx_univer_sheets_tenant ON univer_sheets(tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_univer_sheets_deleted_at ON univer_sheets(deleted_at);
