-- Model(s) that actually produced the diagnosis, as reported by the CLI.
ALTER TABLE investigations ADD COLUMN model TEXT;
