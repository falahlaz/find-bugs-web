CREATE TABLE users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    role          TEXT NOT NULL CHECK (role IN ('qa', 'engineer')),
    active        INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE jobs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id        INTEGER NOT NULL REFERENCES users(id),
    transaction_id TEXT NOT NULL,
    environment    TEXT NOT NULL,
    time_range     TEXT NOT NULL,
    input_kind     TEXT NOT NULL,
    status         TEXT NOT NULL,
    failure_reason TEXT,
    queued_at      TEXT NOT NULL,
    started_at     TEXT,
    vpn_checked_at TEXT,
    search_done_at TEXT,
    analyzed_at    TEXT,
    finished_at    TEXT,
    waiting_since  TEXT
);
CREATE INDEX jobs_status ON jobs(status, queued_at);
CREATE INDEX jobs_user ON jobs(user_id, queued_at);
CREATE INDEX jobs_lookup ON jobs(transaction_id, environment, time_range);

CREATE TABLE investigations (
    job_id           INTEGER PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
    error_type       TEXT,
    failed_component TEXT,
    severity         TEXT,
    summary          TEXT,
    likely_cause     TEXT,
    suggested_action TEXT,
    relevant_logs    TEXT,
    error_source     TEXT,
    raw_log_snippet  TEXT,
    llm_failed       INTEGER NOT NULL DEFAULT 0,
    created_at       TEXT NOT NULL
);

CREATE TABLE audit (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER REFERENCES users(id),
    action  TEXT NOT NULL,
    result  TEXT NOT NULL,
    detail  TEXT,
    at      TEXT NOT NULL
);
CREATE INDEX audit_at ON audit(at);
