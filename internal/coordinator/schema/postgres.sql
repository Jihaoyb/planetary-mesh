CREATE SEQUENCE IF NOT EXISTS job_id_seq;

CREATE TABLE IF NOT EXISTS nodes (
  id TEXT PRIMARY KEY,
  address TEXT NOT NULL,
  last_seen TIMESTAMPTZ NOT NULL,
  state TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '',
  command TEXT NOT NULL DEFAULT '',
  args JSONB NOT NULL DEFAULT '[]'::jsonb,
  status TEXT NOT NULL,
  node_id TEXT NOT NULL DEFAULT '',
  attempts INTEGER NOT NULL DEFAULT 0,
  started_at TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  exit_code INTEGER,
  stdout TEXT NOT NULL DEFAULT '',
  stderr TEXT NOT NULL DEFAULT '',
  stdout_truncated BOOLEAN NOT NULL DEFAULT false,
  stderr_truncated BOOLEAN NOT NULL DEFAULT false,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL
);
