-- Backend IDs followed from the searched transaction ID (JSON array).
ALTER TABLE investigations ADD COLUMN linked_ids TEXT;
