-- Collaborative spreadsheets (Univer). Cell data lives in the collaboration
-- server's own schema; this table only records workspace ownership.
CREATE TABLE IF NOT EXISTS univer_sheets (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    name VARCHAR(200) NOT NULL,
    unit_id VARCHAR(128) NOT NULL,
    creator_id VARCHAR(128) NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    deleted_at DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_univer_sheets_unit_id ON univer_sheets(unit_id);
CREATE INDEX IF NOT EXISTS idx_univer_sheets_tenant ON univer_sheets(tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_univer_sheets_deleted_at ON univer_sheets(deleted_at);
