-- +goose Up
ALTER TABLE loadouts ADD COLUMN is_legal boolean NOT NULL DEFAULT true;

UPDATE loadouts
SET is_legal = jsonb_array_length(COALESCE(risk_summary->'diagnostics', '[]'::jsonb)) = 0;

CREATE INDEX loadouts_public_legal_idx
    ON loadouts (game, published_at DESC, id DESC)
    WHERE status = 'published' AND is_legal;

-- +goose Down
DROP INDEX IF EXISTS loadouts_public_legal_idx;
ALTER TABLE loadouts DROP COLUMN IF EXISTS is_legal;
