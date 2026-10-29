-- name: UserStatus :one
SELECT status FROM users WHERE id = ?;

-- name: ActiveSessionCount :one
SELECT count(*) FROM sessions s JOIN users u ON u.id=s.user_id
WHERE s.user_id=? AND s.revoked_at IS NULL AND s.expires_at>? AND u.status='active';
