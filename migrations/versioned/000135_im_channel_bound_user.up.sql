-- Migration: 000135_im_channel_bound_user
-- IM callbacks are authenticated by the platform signature, not a WeKnora
-- session, so OAuth-protected MCP services had no user to authorize them.
-- A channel now records the WeKnora user who bound it; every turn runs with
-- that user's tenant role, MCP OAuth tokens and session ownership. Existing
-- rows stay '' and keep the legacy synthetic system-<tenantID> identity.
DO $$ BEGIN RAISE NOTICE '[Migration 000135] Adding im_channels.bound_user_id'; END $$;

ALTER TABLE im_channels ADD COLUMN IF NOT EXISTS bound_user_id VARCHAR(36) NOT NULL DEFAULT '';

COMMENT ON COLUMN im_channels.bound_user_id IS 'WeKnora user whose permissions IM turns run as (the channel binder); empty = legacy synthetic identity';
