CREATE SEQUENCE IF NOT EXISTS job_id_seq;

CREATE TABLE IF NOT EXISTS schema_version (
  id TEXT PRIMARY KEY,
  version INTEGER NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS nodes (
  id TEXT PRIMARY KEY,
  address TEXT NOT NULL,
  last_seen TIMESTAMPTZ NOT NULL,
  state TEXT NOT NULL,
  certificate_subject TEXT NOT NULL DEFAULT '',
  certificate_dns_names JSONB NOT NULL DEFAULT '[]'::jsonb,
  certificate_ip_addresses JSONB NOT NULL DEFAULT '[]'::jsonb,
  certificate_uris JSONB NOT NULL DEFAULT '[]'::jsonb,
  certificate_sha256_fingerprint TEXT NOT NULL DEFAULT '',
  certificate_not_after TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS certificate_subject TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS certificate_dns_names JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS certificate_ip_addresses JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS certificate_uris JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS certificate_sha256_fingerprint TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS certificate_not_after TIMESTAMPTZ;

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
