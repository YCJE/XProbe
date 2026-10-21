-- 0001_init.sql — initial schema (design doc §5.6, v1.4)
-- M1 is pre-release: the security columns are part of the initial schema.
-- After the first release, schema changes must go into a new numbered migration.

CREATE TABLE agents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT UNIQUE NOT NULL,          -- SHA-256(Agent Token); plaintext is returned once
    hostname TEXT NOT NULL,
    display_name TEXT,
    os TEXT,
    arch TEXT,
    agent_version TEXT,
    host_fingerprint TEXT,
    ipv4 TEXT,
    ipv6 TEXT,
    geo_lat REAL,
    geo_lon REAL,
    region TEXT,
    country_code TEXT,
    isp TEXT,
    tag_ids TEXT NOT NULL DEFAULT '[]',
    expires_at INTEGER,
    price_amount REAL,
    price_currency TEXT,
    price_cycle TEXT,
    traffic_quota_bytes INTEGER NOT NULL DEFAULT 0,
    last_seen INTEGER,
    online INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    UNIQUE(host_fingerprint)
);

CREATE TABLE tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    color TEXT,
    created_at INTEGER NOT NULL
);

CREATE TABLE traffic_monthly (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id INTEGER NOT NULL,
    month TEXT NOT NULL,
    rx_bytes INTEGER NOT NULL,
    tx_bytes INTEGER NOT NULL,
    UNIQUE(agent_id, month),
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);

CREATE TABLE register_codes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code_hash TEXT UNIQUE NOT NULL,          -- SHA-256(normalized code); never store plaintext
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used INTEGER NOT NULL DEFAULT 0,
    used_by_agent_id INTEGER
);
CREATE INDEX idx_register_codes_expires ON register_codes(expires_at);

CREATE TABLE alert_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    metric TEXT NOT NULL,
    operator TEXT NOT NULL,
    threshold REAL NOT NULL,
    duration INTEGER NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    scope TEXT NOT NULL DEFAULT '{"type":"all"}',
    notify_channel_ids TEXT NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL
);

CREATE TABLE alert_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL,
    agent_id INTEGER NOT NULL,
    status TEXT NOT NULL,
    value REAL,
    started_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    notified INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (rule_id) REFERENCES alert_rules(id)
);
CREATE INDEX idx_alert_history_agent ON alert_history(agent_id, updated_at);

CREATE TABLE notify_channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    config TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE ping_targets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    target TEXT NOT NULL,
    name TEXT NOT NULL,
    region TEXT,
    isp TEXT,
    ip_version INTEGER NOT NULL DEFAULT 4,
    protocol TEXT NOT NULL DEFAULT 'icmp',
    is_default INTEGER NOT NULL DEFAULT 0,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);

CREATE TABLE metric_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id INTEGER NOT NULL,
    timestamp INTEGER NOT NULL,
    cpu_usage REAL,
    mem_usage REAL,
    disk_usage TEXT,
    net_rx INTEGER,
    net_tx INTEGER,
    ping_data TEXT,
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);
CREATE INDEX idx_metric_records_agent_time ON metric_records(agent_id, timestamp);

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,                     -- random UUID for the session list / revocation
    token_hash TEXT UNIQUE NOT NULL,         -- SHA-256(opaque cookie token)
    admin_id INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,             -- absolute expiry
    idle_expires_at INTEGER NOT NULL,        -- idle expiry, refreshed by activity
    revoked INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    last_seen_at INTEGER,
    ip TEXT,
    user_agent TEXT,
    FOREIGN KEY (admin_id) REFERENCES admin(id)
);
CREATE INDEX idx_sessions_admin ON sessions(admin_id, expires_at);
CREATE INDEX idx_sessions_token_hash ON sessions(token_hash);

CREATE TABLE admin (
    id INTEGER PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    totp_secret TEXT,
    totp_enabled INTEGER NOT NULL DEFAULT 0,
    totp_recovery_codes TEXT,
    created_at INTEGER NOT NULL
);

CREATE TABLE share_pages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    share_id TEXT UNIQUE NOT NULL,
    title TEXT,
    logo_url TEXT,                          -- local/embedded asset only; no external URL
    footer_text TEXT,
    agent_ids TEXT,
    created_at INTEGER NOT NULL
);

CREATE TABLE settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Seed default ping targets (is_default=1: 可停用不可删除, 保证开箱即用, §5.6)
INSERT INTO ping_targets (target, name, region, isp, ip_version, protocol, is_default, enabled, created_at) VALUES
    ('114.114.114.114',    '电信',       NULL, 'ct', 4, 'icmp', 1, 1, strftime('%s','now')),
    ('223.5.5.5',          '阿里',       NULL, 'other', 4, 'icmp', 1, 1, strftime('%s','now')),
    ('119.29.29.29',       '腾讯',       NULL, 'other', 4, 'icmp', 1, 1, strftime('%s','now')),
    ('2400:3200::1',       '阿里v6',     NULL, 'other', 6, 'icmp', 1, 1, strftime('%s','now')),
    ('2001:4860:4860::8888','Google v6', NULL, 'other', 6, 'icmp', 1, 1, strftime('%s','now'));
