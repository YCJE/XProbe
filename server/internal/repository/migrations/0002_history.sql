-- 0002_history.sql — ping samples, metric bucket uniqueness and audit log.

CREATE TABLE IF NOT EXISTS ping_records (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_id INTEGER NOT NULL,
    target TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    method TEXT NOT NULL DEFAULT '',
    ip_version INTEGER NOT NULL DEFAULT 4,
    timestamp INTEGER NOT NULL,
    avg_latency REAL NOT NULL DEFAULT 0,
    min_latency REAL NOT NULL DEFAULT 0,
    max_latency REAL NOT NULL DEFAULT 0,
    jitter REAL NOT NULL DEFAULT 0,
    loss REAL NOT NULL DEFAULT 0,
    packets_sent INTEGER NOT NULL DEFAULT 0,
    packets_recv INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_ping_records_agent_time
    ON ping_records(agent_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_ping_records_agent_target_time
    ON ping_records(agent_id, target, timestamp);

-- Keep one row per bucket before adding the uniqueness constraint. This makes
-- the migration safe for databases created before the bucket writer existed.
DELETE FROM metric_records
WHERE id NOT IN (
    SELECT MIN(id) FROM metric_records GROUP BY agent_id, timestamp
);

CREATE UNIQUE INDEX IF NOT EXISTS ux_metric_records_agent_bucket
    ON metric_records(agent_id, timestamp);

CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp INTEGER NOT NULL,
    actor TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL,
    target TEXT NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_log_time ON audit_log(timestamp);
