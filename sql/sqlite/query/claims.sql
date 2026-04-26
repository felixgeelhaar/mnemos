-- name: UpsertClaim :exec
INSERT INTO claims (id, text, type, confidence, status, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  text = excluded.text,
  type = excluded.type,
  confidence = excluded.confidence,
  status = excluded.status,
  created_at = excluded.created_at,
  created_by = excluded.created_by;

-- name: UpsertClaimEvidence :exec
INSERT INTO claim_evidence (claim_id, event_id)
VALUES (?, ?)
ON CONFLICT(claim_id, event_id) DO NOTHING;

-- name: ListAllClaims :many
SELECT id, text, type, confidence, status, created_at, created_by
FROM claims
ORDER BY created_at ASC;

-- name: DeleteClaimByID :exec
DELETE FROM claims WHERE id = ?;

-- name: DeleteAllClaims :exec
DELETE FROM claims;

-- name: DeleteClaimEvidenceByClaimID :exec
DELETE FROM claim_evidence WHERE claim_id = ?;

-- name: DeleteAllClaimEvidence :exec
DELETE FROM claim_evidence;

-- name: DeleteClaimStatusHistoryByClaimID :exec
DELETE FROM claim_status_history WHERE claim_id = ?;

-- name: DeleteAllClaimStatusHistory :exec
DELETE FROM claim_status_history;
