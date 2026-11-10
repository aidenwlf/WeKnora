UPDATE sessions
SET user_id = (
        SELECT ch.bound_user_id
        FROM im_channel_sessions ics
        JOIN im_channels ch ON ch.id = ics.im_channel_id
        WHERE ics.session_id = sessions.id AND ch.bound_user_id <> ''
        LIMIT 1
    ),
    owner_auth_source = CASE WHEN COALESCE(user_id, '') = '' THEN 'im' ELSE owner_auth_source END,
    updated_at = CURRENT_TIMESTAMP
WHERE EXISTS (
    SELECT 1
    FROM im_channel_sessions ics
    JOIN im_channels ch ON ch.id = ics.im_channel_id
    WHERE ics.session_id = sessions.id AND ch.bound_user_id <> ''
);
