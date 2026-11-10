-- Collaborative knowledge base entries (see migrations/versioned/000140).
CREATE TABLE IF NOT EXISTS univer_units (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    name VARCHAR(200) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    unit_id VARCHAR(128) NOT NULL,
    creator_id VARCHAR(128) NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    deleted_at DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_univer_units_unit_id ON univer_units(unit_id);
CREATE INDEX IF NOT EXISTS idx_univer_units_kb ON univer_units(knowledge_base_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_univer_units_tenant ON univer_units(tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_univer_units_deleted_at ON univer_units(deleted_at);
DROP TABLE IF EXISTS univer_sheets;
