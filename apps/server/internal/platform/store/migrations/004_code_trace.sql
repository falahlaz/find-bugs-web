-- Where an internal error was traced to in the service code (JSON), if tried.
ALTER TABLE investigations ADD COLUMN code_trace TEXT;
