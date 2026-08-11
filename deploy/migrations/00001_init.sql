-- +goose Up
-- 00001_init.sql — Aetheria initial schema (brief §5, core tables).
-- Extend via numbered migrations only; never edit an applied migration.

-- ============================== accounts ==============================
CREATE TABLE accounts (
    id              BIGSERIAL PRIMARY KEY,
    email           TEXT NOT NULL UNIQUE,
    pass_hash       TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    banned_until    TIMESTAMPTZ,
    ban_reason      TEXT,
    vote_points     BIGINT NOT NULL DEFAULT 0,
    donation_credits BIGINT NOT NULL DEFAULT 0,
    is_gm           BOOLEAN NOT NULL DEFAULT false,
    totp_secret     TEXT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================ characters ==============================
CREATE TABLE characters (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT NOT NULL REFERENCES accounts(id),
    name            TEXT NOT NULL UNIQUE,
    class           TEXT NOT NULL,
    level           INT NOT NULL DEFAULT 1,
    xp              BIGINT NOT NULL DEFAULT 0,
    gold            BIGINT NOT NULL DEFAULT 0,
    zone_id         TEXT NOT NULL DEFAULT 'havenport',
    pos_x           DOUBLE PRECISION NOT NULL DEFAULT 0,
    pos_y           DOUBLE PRECISION NOT NULL DEFAULT 0,
    pos_z           DOUBLE PRECISION NOT NULL DEFAULT 0,
    hp              BIGINT NOT NULL DEFAULT 100,
    mp              BIGINT NOT NULL DEFAULT 50,
    stats           JSONB NOT NULL DEFAULT '{}',
    playtime_seconds BIGINT NOT NULL DEFAULT 0,
    deleted_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_characters_account ON characters(account_id);

-- ============================ item instances ==========================
CREATE TABLE item_instances (
    id              BIGSERIAL PRIMARY KEY,
    owner_char_id   BIGINT REFERENCES characters(id),
    container       TEXT NOT NULL,  -- inventory|equipment|bank|trade_escrow|auction_escrow
    slot            INT NOT NULL DEFAULT 0,
    item_def_id     TEXT NOT NULL,
    quantity        INT NOT NULL DEFAULT 1,
    bound           BOOLEAN NOT NULL DEFAULT false,
    rolled_stats    JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_item_owner ON item_instances(owner_char_id, container);

-- ============================ content defs ============================
-- Loaded from shared/content seeds; mirrored in DB for admin/auction queries.
CREATE TABLE item_defs (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    type            TEXT NOT NULL,  -- weapon|armor|accessory|consumable|quest|misc
    stackable       BOOLEAN NOT NULL DEFAULT false,
    base_stats      JSONB NOT NULL DEFAULT '{}',
    vendor_price    BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE mob_defs (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    level           INT NOT NULL,
    hp              BIGINT NOT NULL DEFAULT 10,
    mp              BIGINT NOT NULL DEFAULT 0,
    zone_id         TEXT NOT NULL,
    aggro_radius    DOUBLE PRECISION NOT NULL DEFAULT 10,
    leash_radius    DOUBLE PRECISION NOT NULL DEFAULT 20,
    skills          JSONB NOT NULL DEFAULT '[]',
    xp_reward       BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE skill_defs (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    class           TEXT NOT NULL,
    rank            INT NOT NULL DEFAULT 1,
    kind            TEXT NOT NULL,  -- auto|tabtarget|aimed|pbaoe|self
    range           DOUBLE PRECISION NOT NULL DEFAULT 0,
    cooldown_ms     BIGINT NOT NULL DEFAULT 0,
    cost_mp         BIGINT NOT NULL DEFAULT 0,
    power           BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE quest_defs (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    min_level       INT NOT NULL DEFAULT 1,
    objectives      JSONB NOT NULL DEFAULT '[]',
    rewards         JSONB NOT NULL DEFAULT '{}',
    next_quest      TEXT
);

CREATE TABLE npc_defs (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    zone_id         TEXT NOT NULL,
    kind            TEXT NOT NULL  -- trainer|vendor|auctioneer|banker|quest|shrine
);

CREATE TABLE zone_defs (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    safe            BOOLEAN NOT NULL DEFAULT false,
    size_x          DOUBLE PRECISION NOT NULL DEFAULT 0,
    size_z          DOUBLE PRECISION NOT NULL DEFAULT 0
);

CREATE TABLE drop_tables (
    id              TEXT PRIMARY KEY,
    mob_def_id      TEXT NOT NULL,
    item_def_id     TEXT NOT NULL,
    chance          DOUBLE PRECISION NOT NULL DEFAULT 0,
    min_qty         INT NOT NULL DEFAULT 1,
    max_qty         INT NOT NULL DEFAULT 1
);

-- ========================= character progress =========================
CREATE TABLE character_quests (
    char_id         BIGINT NOT NULL REFERENCES characters(id),
    quest_id        TEXT NOT NULL,
    state           TEXT NOT NULL DEFAULT 'active',  -- active|complete|abandoned
    progress        JSONB NOT NULL DEFAULT '{}',
    PRIMARY KEY (char_id, quest_id)
);

CREATE TABLE character_skills (
    char_id         BIGINT NOT NULL REFERENCES characters(id),
    skill_id        TEXT NOT NULL,
    rank            INT NOT NULL DEFAULT 1,
    PRIMARY KEY (char_id, skill_id)
);

-- ============================= social ================================
CREATE TABLE friends (
    char_id         BIGINT NOT NULL REFERENCES characters(id),
    friend_char_id  BIGINT NOT NULL REFERENCES characters(id),
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending|accepted|blocked
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (char_id, friend_char_id)
);

-- ============================= auction ================================
CREATE TABLE auction_listings (
    id              BIGSERIAL PRIMARY KEY,
    seller_char_id  BIGINT NOT NULL REFERENCES characters(id),
    item_instance_id BIGINT NOT NULL REFERENCES item_instances(id),
    buyout_price    BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    state           TEXT NOT NULL DEFAULT 'active'  -- active|sold|expired|withdrawn
);
CREATE INDEX idx_auction_active ON auction_listings(state, expires_at);

-- ========================== economy / audit ===========================
CREATE TABLE gold_ledger (
    id              BIGSERIAL PRIMARY KEY,
    char_id         BIGINT,
    amount          BIGINT NOT NULL,  -- signed delta; + created, - destroyed
    reason          TEXT NOT NULL,
    ref_id          BIGINT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_gold_ledger_char ON gold_ledger(char_id, created_at);

CREATE TABLE trade_log (
    id              BIGSERIAL PRIMARY KEY,
    char_a_id       BIGINT NOT NULL,
    char_b_id       BIGINT NOT NULL,
    items           JSONB NOT NULL DEFAULT '[]',
    gold_a_to_b     BIGINT NOT NULL DEFAULT 0,
    gold_b_to_a     BIGINT NOT NULL DEFAULT 0,
    outcome         TEXT NOT NULL DEFAULT 'completed',  -- completed|cancelled|timeout
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE audit_log (
    id              BIGSERIAL PRIMARY KEY,
    actor_account_id BIGINT REFERENCES accounts(id),
    actor_char_id   BIGINT REFERENCES characters(id),
    action          TEXT NOT NULL,
    detail          JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE logins (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT NOT NULL REFERENCES accounts(id),
    ip              TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE votes (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT NOT NULL REFERENCES accounts(id),
    site            TEXT NOT NULL,
    ip              TEXT,
    credited_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_votes_replay ON votes(account_id, site, ip);

CREATE TABLE donations (
    id              BIGSERIAL PRIMARY KEY,
    account_id      BIGINT NOT NULL REFERENCES accounts(id),
    provider        TEXT NOT NULL,
    amount          BIGINT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',  -- pending|approved|declined
    external_ref    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE client_builds (
    id              BIGSERIAL PRIMARY KEY,
    version         TEXT NOT NULL,
    platform        TEXT NOT NULL,  -- linux|windows
    file_path       TEXT NOT NULL,
    sha256          TEXT NOT NULL,
    published_at    TIMESTAMPTZ
);

-- +goose Down
-- (down migration intentionally empty for init; use numbered rollback migrations)
