-- +goose Up
ALTER TABLE users ADD COLUMN email_verified_at timestamptz;

CREATE TABLE email_provider_settings (
    id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    provider varchar(24) NOT NULL DEFAULT 'aoksend' CHECK (provider = 'aoksend'),
    enabled boolean NOT NULL DEFAULT false,
    api_key_ciphertext bytea,
    api_key_nonce bytea,
    template_id varchar(80) NOT NULL DEFAULT '',
    sender_alias varchar(80) NOT NULL DEFAULT 'MHED',
    reply_to varchar(320) NOT NULL DEFAULT '',
    updated_by uuid REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((api_key_ciphertext IS NULL) = (api_key_nonce IS NULL))
);
INSERT INTO email_provider_settings (id) VALUES (1);

CREATE TABLE email_verification_challenges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    purpose varchar(24) NOT NULL CHECK (purpose IN ('register','bind_email','reset_password')),
    user_id uuid REFERENCES users(id) ON DELETE CASCADE,
    username varchar(32) NOT NULL,
    email varchar(320) NOT NULL,
    code_digest bytea NOT NULL,
    source_digest bytea NOT NULL,
    status varchar(16) NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','sent','used','failed')),
    attempt_count smallint NOT NULL DEFAULT 0 CHECK (attempt_count BETWEEN 0 AND 5),
    sent_at timestamptz,
    expires_at timestamptz,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX email_challenges_email_rate_idx
    ON email_verification_challenges (lower(email), created_at DESC);
CREATE INDEX email_challenges_source_rate_idx
    ON email_verification_challenges (source_digest, created_at DESC);
CREATE INDEX email_challenges_user_idx
    ON email_verification_challenges (user_id, created_at DESC) WHERE user_id IS NOT NULL;

CREATE TABLE email_outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    challenge_id uuid REFERENCES email_verification_challenges(id) ON DELETE CASCADE,
    recipient varchar(320) NOT NULL,
    payload_ciphertext bytea,
    payload_nonce bytea,
    status varchar(16) NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','sending','sent','failed')),
    attempt_count smallint NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    provider_message_id varchar(160) NOT NULL DEFAULT '',
    last_error_code varchar(80) NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    CHECK ((payload_ciphertext IS NULL) = (payload_nonce IS NULL))
);
CREATE INDEX email_outbox_pending_idx ON email_outbox (next_attempt_at, id)
    WHERE status IN ('queued','sending');

-- +goose Down
DROP TABLE IF EXISTS email_outbox;
DROP TABLE IF EXISTS email_verification_challenges;
DROP TABLE IF EXISTS email_provider_settings;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
