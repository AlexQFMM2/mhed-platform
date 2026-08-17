-- +goose Up
ALTER TABLE users ADD COLUMN public_id bigint GENERATED ALWAYS AS IDENTITY;
ALTER TABLE users ADD COLUMN nickname varchar(32);

UPDATE users SET nickname = '用户' || public_id::text WHERE nickname IS NULL;
SET CONSTRAINTS users_keep_super_admin IMMEDIATE;

ALTER TABLE users ALTER COLUMN nickname SET NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_nickname_length CHECK (char_length(btrim(nickname)) BETWEEN 2 AND 32);
ALTER TABLE users ADD CONSTRAINT users_nickname_no_control CHECK (nickname !~ '[[:cntrl:]]');
CREATE UNIQUE INDEX users_public_id_unique ON users (public_id);

-- +goose StatementBegin
CREATE FUNCTION assign_default_user_nickname() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.nickname IS NULL OR btrim(NEW.nickname) = '' THEN
        NEW.nickname := '用户' || NEW.public_id::text;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER users_assign_default_nickname
    BEFORE INSERT ON users FOR EACH ROW EXECUTE FUNCTION assign_default_user_nickname();

ALTER TABLE sessions ADD COLUMN client_type varchar(16) NOT NULL DEFAULT 'browser'
    CHECK (client_type IN ('browser', 'desktop'));

ALTER TABLE loadouts ADD COLUMN build_hash bytea;
UPDATE loadouts SET build_hash = content_hash WHERE build_hash IS NULL;
ALTER TABLE loadouts ALTER COLUMN build_hash SET NOT NULL;
CREATE UNIQUE INDEX loadouts_unique_build_idx
    ON loadouts (game, data_version, build_hash)
    WHERE status <> 'deleted';

CREATE TABLE loadout_likes (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    loadout_id uuid NOT NULL REFERENCES loadouts(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, loadout_id)
);
CREATE INDEX loadout_likes_ranking_idx ON loadout_likes (loadout_id, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS loadout_likes;
DROP INDEX IF EXISTS loadouts_unique_build_idx;
ALTER TABLE loadouts DROP COLUMN IF EXISTS build_hash;
ALTER TABLE sessions DROP COLUMN IF EXISTS client_type;
DROP TRIGGER IF EXISTS users_assign_default_nickname ON users;
DROP FUNCTION IF EXISTS assign_default_user_nickname();
DROP INDEX IF EXISTS users_public_id_unique;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_nickname_no_control;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_nickname_length;
ALTER TABLE users DROP COLUMN IF EXISTS nickname;
ALTER TABLE users DROP COLUMN IF EXISTS public_id;
