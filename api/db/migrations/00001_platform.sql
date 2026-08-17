-- +goose Up
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username varchar(32) NOT NULL CHECK (username ~ '^[A-Za-z0-9_]{3,32}$'),
    email varchar(320),
    password_hash text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'deleted')),
    must_change_password boolean NOT NULL DEFAULT true,
    created_by uuid REFERENCES users(id),
    last_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE UNIQUE INDEX users_username_unique_ci ON users (lower(username));
CREATE UNIQUE INDEX users_email_unique_ci ON users (lower(email)) WHERE email IS NOT NULL AND status <> 'deleted';

CREATE TABLE roles (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key varchar(64) NOT NULL UNIQUE CHECK (key ~ '^[a-z][a-z0-9_.-]{1,63}$'),
    name varchar(80) NOT NULL,
    description varchar(300) NOT NULL DEFAULT '',
    is_system boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE permissions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    key varchar(80) NOT NULL UNIQUE CHECK (key ~ '^[a-z][a-z0-9_.-]{1,79}$'),
    name varchar(100) NOT NULL,
    description varchar(300) NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE user_roles (
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    assigned_by uuid REFERENCES users(id),
    assigned_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);
CREATE TABLE role_permissions (
    role_id uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id uuid NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE TABLE sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    csrf_hash bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    idle_expires_at timestamptz NOT NULL,
    absolute_expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    user_agent varchar(300) NOT NULL DEFAULT '',
    ip_digest bytea
);
CREATE INDEX sessions_user_active_idx ON sessions (user_id, absolute_expires_at) WHERE revoked_at IS NULL;

CREATE TABLE loadouts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id uuid NOT NULL REFERENCES users(id),
    game varchar(24) NOT NULL CHECK (game = 'mh3g'),
    schema_version smallint NOT NULL CHECK (schema_version = 1),
    data_version varchar(64) NOT NULL,
    name varchar(40) NOT NULL CHECK (char_length(name) BETWEEN 1 AND 40),
    remark varchar(500) NOT NULL DEFAULT '' CHECK (char_length(remark) <= 500),
    payload jsonb NOT NULL,
    content_hash bytea NOT NULL,
    risk_summary jsonb NOT NULL DEFAULT '{}'::jsonb,
    status varchar(16) NOT NULL DEFAULT 'published' CHECK (status IN ('published', 'hidden', 'deleted')),
    version integer NOT NULL DEFAULT 1,
    published_at timestamptz,
    created_by uuid NOT NULL REFERENCES users(id),
    updated_by uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);
CREATE INDEX loadouts_public_idx ON loadouts (game, published_at DESC, id DESC) WHERE status = 'published';
CREATE INDEX loadouts_owner_idx ON loadouts (owner_user_id, updated_at DESC);
CREATE INDEX loadouts_name_search_idx ON loadouts USING gin (to_tsvector('simple', name || ' ' || remark));
CREATE INDEX loadouts_content_hash_idx ON loadouts (content_hash);

CREATE TABLE loadout_equipment_index (
    loadout_id uuid NOT NULL REFERENCES loadouts(id) ON DELETE CASCADE,
    slot varchar(12) NOT NULL CHECK (slot IN ('weapon','head','chest','arms','waist','legs','charm')),
    save_type smallint NOT NULL,
    save_id integer NOT NULL,
    PRIMARY KEY (loadout_id, slot)
);
CREATE INDEX loadout_equipment_lookup_idx ON loadout_equipment_index (slot, save_type, save_id, loadout_id);
CREATE TABLE loadout_skill_index (
    loadout_id uuid NOT NULL REFERENCES loadouts(id) ON DELETE CASCADE,
    skill_tree_id integer NOT NULL,
    points integer NOT NULL,
    active_skill_id integer,
    PRIMARY KEY (loadout_id, skill_tree_id)
);
CREATE INDEX loadout_skill_lookup_idx ON loadout_skill_index (skill_tree_id, points, loadout_id);
CREATE INDEX loadout_active_skill_lookup_idx ON loadout_skill_index (active_skill_id, loadout_id) WHERE active_skill_id IS NOT NULL;

CREATE TABLE loadout_reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    loadout_id uuid NOT NULL REFERENCES loadouts(id),
    reporter_user_id uuid REFERENCES users(id),
    source_digest bytea NOT NULL,
    reason varchar(32) NOT NULL CHECK (reason IN ('inappropriate','spam','invalid_data','infringement','other')),
    details varchar(500) NOT NULL DEFAULT '' CHECK (char_length(details) <= 500),
    evidence_name varchar(40) NOT NULL,
    evidence_remark varchar(500) NOT NULL,
    evidence_content_hash bytea NOT NULL,
    status varchar(16) NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','dismissed')),
    handled_by uuid REFERENCES users(id),
    resolution_note varchar(500) NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    handled_at timestamptz
);
CREATE UNIQUE INDEX loadout_reports_open_source_idx ON loadout_reports (loadout_id, source_digest) WHERE status = 'open';
CREATE INDEX loadout_reports_status_idx ON loadout_reports (status, created_at DESC);

CREATE TABLE admin_audit_logs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_user_id uuid REFERENCES users(id),
    action varchar(80) NOT NULL,
    target_type varchar(40) NOT NULL,
    target_id text NOT NULL,
    request_id varchar(100) NOT NULL DEFAULT '',
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_audit_logs_created_idx ON admin_audit_logs (created_at DESC, id DESC);
CREATE INDEX admin_audit_logs_target_idx ON admin_audit_logs (target_type, target_id, created_at DESC);

INSERT INTO roles (key, name, description, is_system)
VALUES ('super_admin', '超级管理员', '可访问管理后台并管理平台全部资源。', true);
INSERT INTO permissions (key, name, description) VALUES
    ('user.manage', '用户管理', '创建、禁用和恢复用户。'),
    ('role.manage', '角色管理', '管理角色、权限和成员。'),
    ('loadout.moderate', '配装审核', '隐藏、恢复、删除配装并处理举报。'),
    ('loadout.feature', '配装推荐', '管理配装展示权重。'),
    ('audit.read', '审计查看', '查看管理操作日志。');
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p WHERE r.key = 'super_admin';

-- +goose StatementBegin
CREATE FUNCTION protect_system_roles() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.is_system AND (TG_OP = 'DELETE' OR NEW.key <> OLD.key OR NOT NEW.is_system) THEN
        RAISE EXCEPTION 'system role cannot be renamed, demoted, or deleted';
    END IF;
    RETURN COALESCE(NEW, OLD);
END $$;
-- +goose StatementEnd
CREATE TRIGGER roles_protect_system BEFORE UPDATE OR DELETE ON roles FOR EACH ROW EXECUTE FUNCTION protect_system_roles();

-- +goose StatementBegin
CREATE FUNCTION ensure_active_super_admin() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE active_count integer;
BEGIN
    SELECT count(*) INTO active_count
      FROM users u JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id
     WHERE r.key = 'super_admin' AND u.status = 'active';
    IF active_count = 0 THEN
        RAISE EXCEPTION 'at least one active super_admin is required';
    END IF;
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE CONSTRAINT TRIGGER user_roles_keep_super_admin AFTER DELETE OR UPDATE ON user_roles
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION ensure_active_super_admin();
CREATE CONSTRAINT TRIGGER users_keep_super_admin AFTER UPDATE OR DELETE ON users
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION ensure_active_super_admin();

-- +goose Down
DROP TABLE IF EXISTS admin_audit_logs;
DROP TABLE IF EXISTS loadout_reports;
DROP TABLE IF EXISTS loadout_skill_index;
DROP TABLE IF EXISTS loadout_equipment_index;
DROP TABLE IF EXISTS loadouts;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS users;
DROP FUNCTION IF EXISTS ensure_active_super_admin();
DROP FUNCTION IF EXISTS protect_system_roles();
