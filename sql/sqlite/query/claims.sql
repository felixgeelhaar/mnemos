-- name: UpsertClaim :exec
-- ON CONFLICT preserves trust_score and valid_to (computed/managed
-- separately via UpdateClaimTrust and SetClaimValidity), but does
-- refresh valid_from: re-extracting a claim with newer evidence is
-- a legitimate "this fact is observed again from <ts>" signal.
INSERT INTO claims (id, text, type, confidence, status, created_at, created_by, valid_from)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  text = excluded.text,
  type = excluded.type,
  confidence = excluded.confidence,
  status = excluded.status,
  created_at = excluded.created_at,
  created_by = excluded.created_by,
  valid_from = excluded.valid_from;

-- name: SetClaimValidity :exec
-- Atomic supersession primitive: mark a claim as no longer valid as
-- of the given timestamp. Pass NULL to clear valid_to (un-supersede
-- the claim), useful when a resolution is reverted.
UPDATE claims SET valid_to = ? WHERE id = ?;

-- name: UpsertClaimEvidence :exec
INSERT INTO claim_evidence (claim_id, event_id)
VALUES (?, ?)
ON CONFLICT(claim_id, event_id) DO NOTHING;

-- name: ListAllClaims :many
SELECT id, text, type, confidence, status, created_at, created_by, trust_score,
       valid_from, valid_to
FROM claims
ORDER BY created_at ASC;

-- name: UpdateClaimTrust :exec
UPDATE claims SET trust_score = ? WHERE id = ?;

-- name: ListClaimTrustInputs :many
-- Inputs to recompute trust_score for every claim: confidence, the
-- distinct evidence-event count, and the most-recent evidence event
-- timestamp. LEFT JOIN so claims with no evidence still appear; the
-- caller treats the missing aggregate as 0/empty.
SELECT
  c.id              AS claim_id,
  c.confidence      AS confidence,
  COUNT(DISTINCT ce.event_id) AS evidence_count,
  CAST(COALESCE(MAX(e.timestamp), '') AS TEXT) AS latest_evidence_at
FROM claims c
LEFT JOIN claim_evidence ce ON ce.claim_id = c.id
LEFT JOIN events e          ON e.id = ce.event_id
GROUP BY c.id, c.confidence;

-- name: AverageTrust :one
SELECT CAST(COALESCE(AVG(trust_score), 0) AS REAL) AS avg_trust FROM claims;

-- name: CountClaimsBelowTrust :one
SELECT COUNT(*) AS n FROM claims WHERE trust_score < ?;

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
