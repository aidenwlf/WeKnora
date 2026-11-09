-- Migration: 000138_im_channel_session_owner
-- A channel's conversations belong to the user who bound it. Move every
-- session produced by a bound channel to that user so the session owner scope
-- (read, write, list) matches the channel binding. Unbound legacy channels are
-- left untouched.
DO $$ BEGIN RAISE NOTICE '[Migration 000138] Backfilling IM session owners'; END $$;

UPDATE sessions AS s
SET user_id = ch.bound_user_id,
    owner_auth_source = CASE WHEN COALESCE(s.user_id, '') = '' THEN 'im' ELSE s.owner_auth_source END,
    updated_at = CURRENT_TIMESTAMP
FROM im_channel_sessions AS ics
JOIN im_channels AS ch ON ch.id = ics.im_channel_id
WHERE ics.session_id = s.id
  AND ch.bound_user_id <> ''
  AND COALESCE(s.user_id, '') <> ch.bound_user_id;
