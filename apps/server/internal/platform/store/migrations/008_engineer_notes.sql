-- Engineer recommendations for an error, matched to jobs by the normalized
-- failed component and error type. Not tied to a job: diagnoses are purged.
CREATE TABLE engineer_notes (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    component_key     TEXT NOT NULL,
    error_type_key    TEXT NOT NULL,
    component         TEXT NOT NULL,
    error_type        TEXT NOT NULL,
    error_source      TEXT NOT NULL DEFAULT '',
    cause             TEXT NOT NULL,
    qa_recommendation TEXT NOT NULL,
    cause_text        TEXT NOT NULL,
    qa_text           TEXT NOT NULL,
    ai_verdict        TEXT NOT NULL CHECK (ai_verdict IN ('ok', 'overridden', 'unchecked')),
    source_job_id     INTEGER REFERENCES jobs(id) ON DELETE SET NULL,
    state             TEXT NOT NULL CHECK (state IN ('active', 'archived')),
    created_by        INTEGER REFERENCES users(id),
    updated_by        INTEGER REFERENCES users(id),
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);
CREATE UNIQUE INDEX engineer_notes_key ON engineer_notes(component_key, error_type_key) WHERE state = 'active';

-- Every saved revision of a note.
CREATE TABLE engineer_note_versions (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    note_id           INTEGER NOT NULL REFERENCES engineer_notes(id) ON DELETE CASCADE,
    cause             TEXT NOT NULL,
    qa_recommendation TEXT NOT NULL,
    cause_text        TEXT NOT NULL,
    qa_text           TEXT NOT NULL,
    ai_verdict        TEXT NOT NULL,
    user_id           INTEGER REFERENCES users(id),
    created_at        TEXT NOT NULL
);
CREATE INDEX engineer_note_versions_note ON engineer_note_versions(note_id, id);
