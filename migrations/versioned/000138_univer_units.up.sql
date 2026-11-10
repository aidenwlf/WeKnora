-- Collaborative knowledge bases: entries are live Univer units (spreadsheet,
-- document, multi-dimensional table or slide deck) owned by one knowledge base.
-- Cell/paragraph/page data lives in the collaboration server's schema; this
-- table only records ownership.
CREATE TABLE IF NOT EXISTS univer_units (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    name VARCHAR(200) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    unit_id VARCHAR(128) NOT NULL,
    creator_id VARCHAR(128) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_univer_units_unit_id ON univer_units(unit_id);
CREATE INDEX IF NOT EXISTS idx_univer_units_kb ON univer_units(knowledge_base_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_univer_units_tenant ON univer_units(tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_univer_units_deleted_at ON univer_units(deleted_at);

-- Superseded by univer_units (entries now belong to a knowledge base).
DROP TABLE IF EXISTS univer_sheets;
