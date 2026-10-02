-- The Claude Code session a code trace ran in, so engineers can continue it.
CREATE TABLE trace_sessions (
    job_id           INTEGER PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
    session_id       TEXT NOT NULL,
    dir              TEXT NOT NULL,
    repos            TEXT NOT NULL,
    state            TEXT NOT NULL CHECK (state IN ('open', 'closed', 'purged')),
    restart          INTEGER NOT NULL DEFAULT 0,
    busy             INTEGER NOT NULL DEFAULT 0,
    last_activity_at TEXT NOT NULL,
    created_at       TEXT NOT NULL
);

-- Questions, answers and re-traces in a trace session.
CREATE TABLE trace_messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id      INTEGER NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    role        TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    kind        TEXT NOT NULL CHECK (kind IN ('chat', 'retrace')),
    user_id     INTEGER REFERENCES users(id),
    content     TEXT NOT NULL DEFAULT '',
    code_trace  TEXT,
    progress    TEXT NOT NULL DEFAULT '[]',
    status      TEXT NOT NULL CHECK (status IN ('queued', 'running', 'done', 'failed')),
    error       TEXT,
    model       TEXT,
    created_at  TEXT NOT NULL,
    finished_at TEXT
);
CREATE INDEX trace_messages_job ON trace_messages(job_id, id);
