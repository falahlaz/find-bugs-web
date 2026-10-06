-- X-SPRINT-IDENTIFIER header of a pasted curl.
ALTER TABLE jobs ADD COLUMN sprint_identifier TEXT;
-- Pods (<namespace>_<pod>) that logged the transaction (JSON array).
ALTER TABLE investigations ADD COLUMN pods TEXT;
