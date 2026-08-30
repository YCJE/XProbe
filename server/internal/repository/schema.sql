-- XProbe Server 数据库结构(设计文档 5.6, v1.3)。
-- 由 Migrate 幂等执行; Token/注册码/会话仅存哈希(S9)。

-- Agent 元数据
CREATE TABLE IF NOT EXISTS agents (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT UNIQUE NOT NULL,      -- SHA256(原始Token) hex, 不存原文(S9)
    hostname TEXT NOT NULL,
    display_name TEXT,
    os TEXT,
    arch TEXT,
    agent_version TEXT,
    host_fingerprint TEXT,                -- SHA256(安装盐+CPU型号+主网卡MAC+系统类型), 见设计文档 7.5
    ipv4 TEXT,
    ipv6 TEXT,
    region TEXT,
    country_code TEXT,
    isp TEXT,
    tag_ids TEXT,                         -- JSON数组: 标签ID列表
    expires_at INTEGER,
    price_amount REAL,
    price_currency TEXT,
    price_cycle TEXT,
    traffic_quota_bytes INTEGER,          -- 0=不限
    last_seen INTEGER,
    online INTEGER DEFAULT 0,
    created_at INTEGER NOT NULL,
    UNIQUE(host_fingerprint)
);

-- 标签
CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT UNIQUE NOT NULL,
    color TEXT,
    created_at INTEGER NOT NULL
);

-- 月度流量归档(每Agent每月一行, 月界 UTC)
CREATE TABLE IF NOT EXISTS traffic_monthly (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id INTEGER NOT NULL,
    month TEXT NOT NULL,
    rx_bytes INTEGER NOT NULL,
    tx_bytes INTEGER NOT NULL,
    UNIQUE(agent_id, month),
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);

-- 注册码(仅存哈希, S9)
CREATE TABLE IF NOT EXISTS register_codes (
    code_hash TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used INTEGER DEFAULT 0,
    used_by_agent_id INTEGER
);

-- 告警规则
CREATE TABLE IF NOT EXISTS alert_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    metric TEXT NOT NULL,
    operator TEXT NOT NULL,
    threshold REAL NOT NULL,
    duration INTEGER NOT NULL,
    enabled INTEGER DEFAULT 1,
    notify_channel_id INTEGER,
    created_at INTEGER NOT NULL
);

-- 告警历史(状态机持久化)
CREATE TABLE IF NOT EXISTS alert_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id INTEGER NOT NULL,
    agent_id INTEGER NOT NULL,
    status TEXT NOT NULL,
    value REAL,
    started_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    notified INTEGER DEFAULT 0,
    FOREIGN KEY (rule_id) REFERENCES alert_rules(id)
);
CREATE INDEX IF NOT EXISTS idx_alert_history_agent ON alert_history(agent_id, updated_at);

-- 通知渠道
CREATE TABLE IF NOT EXISTS notify_channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    config TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

-- 探测目标
CREATE TABLE IF NOT EXISTS ping_targets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    target TEXT NOT NULL,
    name TEXT NOT NULL,
    region TEXT,
    isp TEXT,
    ip_version INTEGER DEFAULT 4,
    protocol TEXT DEFAULT 'icmp',
    is_default INTEGER DEFAULT 0,
    enabled INTEGER DEFAULT 1,
    created_at INTEGER NOT NULL
);

-- 历史 5 分钟聚合数据(保留 90 天)
CREATE TABLE IF NOT EXISTS metric_records (
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
CREATE INDEX IF NOT EXISTS idx_metric_records_agent_time ON metric_records(agent_id, timestamp);

-- 日聚合数据(保留 365 天, 支撑详情页 7d/30d 视图)
CREATE TABLE IF NOT EXISTS metric_records_daily (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id INTEGER NOT NULL,
    date TEXT NOT NULL,
    cpu_usage_avg REAL,
    cpu_usage_max REAL,
    mem_usage_avg REAL,
    mem_usage_max REAL,
    disk_usage TEXT,
    net_rx_avg INTEGER,
    net_tx_avg INTEGER,
    ping_data TEXT,
    UNIQUE(agent_id, date),
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);
CREATE INDEX IF NOT EXISTS idx_metric_daily_agent_date ON metric_records_daily(agent_id, date);

-- 管理员账户(TOTP 字段随 TOTP 功能在 v2 迁移添加)
CREATE TABLE IF NOT EXISTS admin (
    id INTEGER PRIMARY KEY,
    username TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

-- 登录会话(JWT 吊销依据; 仅存哈希 S9)
CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash TEXT UNIQUE NOT NULL,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked INTEGER DEFAULT 0,
    ip TEXT,
    user_agent TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);

-- 公开分享页配置
CREATE TABLE IF NOT EXISTS share_pages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    share_id TEXT UNIQUE NOT NULL,
    title TEXT,
    logo_url TEXT,
    footer_text TEXT,
    agent_ids TEXT,
    created_at INTEGER NOT NULL
);
