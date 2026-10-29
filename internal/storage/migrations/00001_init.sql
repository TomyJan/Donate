-- +goose Up
CREATE TABLE users (
 id BLOB PRIMARY KEY CHECK(length(id)=16), status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','banned')),
 public_identity_id BLOB, created_at INTEGER NOT NULL, last_login_at INTEGER,
 FOREIGN KEY(public_identity_id) REFERENCES identities(id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE identities (
 id BLOB PRIMARY KEY CHECK(length(id)=16), user_id BLOB NOT NULL REFERENCES users(id),
 provider TEXT NOT NULL CHECK(provider IN ('qq','oidc')), issuer TEXT NOT NULL, subject TEXT NOT NULL,
 nickname TEXT NOT NULL, avatar_url TEXT NOT NULL DEFAULT '', qq_number TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
 UNIQUE(provider,issuer,subject), UNIQUE(user_id,provider)
);
CREATE TABLE sessions (
 id BLOB PRIMARY KEY CHECK(length(id)=16), user_id BLOB NOT NULL REFERENCES users(id), identity_id BLOB NOT NULL REFERENCES identities(id),
 token_hash BLOB NOT NULL UNIQUE CHECK(length(token_hash)=32), csrf_hash BLOB NOT NULL CHECK(length(csrf_hash)=32),
 created_at INTEGER NOT NULL, renewed_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, revoked_at INTEGER
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE auth_flows (
 id BLOB PRIMARY KEY CHECK(length(id)=16), state_hash BLOB NOT NULL UNIQUE, cookie_hash BLOB NOT NULL,
 provider TEXT NOT NULL, intent TEXT NOT NULL CHECK(intent IN ('login','bind')), user_id BLOB REFERENCES users(id), session_id BLOB REFERENCES sessions(id),
 nonce TEXT NOT NULL, secret BLOB NOT NULL, stage TEXT NOT NULL DEFAULT 'pending', attempts INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, consumed_at INTEGER
);
CREATE TABLE orders (
 id BLOB PRIMARY KEY CHECK(length(id)=16), user_id BLOB REFERENCES users(id), merchant_order_no TEXT NOT NULL UNIQUE,
 amount_fen INTEGER NOT NULL CHECK(amount_fen BETWEEN 1 AND 10000000), channel TEXT NOT NULL CHECK(channel IN ('alipay','wxpay')),
 message TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','paid','closed')),
 close_reason TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, paid_at INTEGER, closed_at INTEGER,
 protocol TEXT NOT NULL DEFAULT 'v2', merchant_id TEXT NOT NULL DEFAULT '', gateway_id TEXT NOT NULL DEFAULT '',
 trade_no TEXT, api_trade_no TEXT NOT NULL DEFAULT '', gateway_status TEXT NOT NULL DEFAULT 'unknown',
 last_checked_at INTEGER, remote_close_status TEXT NOT NULL DEFAULT 'unknown', remote_closed_at INTEGER,
 hidden_order INTEGER NOT NULL DEFAULT 0 CHECK(hidden_order IN (0,1)), hidden_message INTEGER NOT NULL DEFAULT 0 CHECK(hidden_message IN (0,1)),
 request_ip TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, submission_status TEXT NOT NULL DEFAULT 'new',
 checkout_url TEXT NOT NULL DEFAULT '', submission_lease TEXT, submission_lease_until INTEGER,
 CHECK(status!='paid' OR (paid_at IS NOT NULL AND trade_no IS NOT NULL)), UNIQUE(gateway_id,merchant_id,trade_no)
);
CREATE INDEX orders_public ON orders(status,hidden_order,paid_at DESC,id DESC);
CREATE INDEX orders_user ON orders(user_id,created_at DESC,id DESC);
CREATE INDEX orders_created ON orders(created_at DESC,id DESC);
CREATE INDEX orders_channel ON orders(channel,status,created_at DESC);
CREATE TABLE order_locations (
 id BLOB PRIMARY KEY CHECK(length(id)=16), order_id BLOB NOT NULL UNIQUE REFERENCES orders(id) ON DELETE CASCADE,
 country TEXT NOT NULL DEFAULT '', province TEXT NOT NULL DEFAULT '', city TEXT NOT NULL DEFAULT '', district TEXT NOT NULL DEFAULT '',
 street TEXT NOT NULL DEFAULT '', isp TEXT NOT NULL DEFAULT '', asn TEXT NOT NULL DEFAULT '', latitude REAL, longitude REAL, accuracy_radius REAL,
 source TEXT NOT NULL, source_version TEXT NOT NULL DEFAULT '', queried_at INTEGER NOT NULL
);
CREATE TABLE jobs (
 id BLOB PRIMARY KEY CHECK(length(id)=16), kind TEXT NOT NULL CHECK(kind IN ('payment','geo','close')),
 order_id BLOB NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
 status TEXT NOT NULL DEFAULT 'ready' CHECK(status IN ('ready','running','done','failed')),
 run_at INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, lease_token TEXT, lease_until INTEGER,
 source TEXT NOT NULL DEFAULT 'automatic', actor_id BLOB REFERENCES users(id), request_id TEXT NOT NULL DEFAULT '',
 rerun INTEGER NOT NULL DEFAULT 0, last_result TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX jobs_active ON jobs(kind,order_id) WHERE status IN ('ready','running');
CREATE INDEX jobs_due ON jobs(status,run_at);
CREATE TABLE payment_observations (
 id BLOB PRIMARY KEY CHECK(length(id)=16), order_id BLOB NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
 source TEXT NOT NULL, request_id TEXT NOT NULL, job_id BLOB, trade_no TEXT NOT NULL, status TEXT NOT NULL,
 amount_fen INTEGER NOT NULL, observed_at INTEGER NOT NULL, result TEXT NOT NULL
);
CREATE TABLE admin_operations (
 id BLOB PRIMARY KEY CHECK(length(id)=16), actor_id BLOB NOT NULL REFERENCES users(id), action TEXT NOT NULL,
 target_ids TEXT NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, confirmed_at INTEGER
);
CREATE TABLE admin_operation_items (
 id BLOB PRIMARY KEY CHECK(length(id)=16), operation_id BLOB NOT NULL REFERENCES admin_operations(id) ON DELETE CASCADE,
 target_id BLOB NOT NULL, result TEXT NOT NULL, job_id BLOB
);
CREATE TABLE audit_events (
 id BLOB PRIMARY KEY CHECK(length(id)=16), actor_id BLOB REFERENCES users(id), action TEXT NOT NULL,
 target_type TEXT NOT NULL, target_id BLOB NOT NULL, result TEXT NOT NULL, created_at INTEGER NOT NULL
);
CREATE INDEX audit_created ON audit_events(created_at DESC,id DESC);
CREATE TABLE idempotency_records (
 id BLOB PRIMARY KEY CHECK(length(id)=16), caller_id BLOB NOT NULL REFERENCES users(id), action TEXT NOT NULL, key TEXT NOT NULL,
 digest BLOB NOT NULL, target_id BLOB, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL,
 UNIQUE(caller_id,action,key)
);
-- +goose Down
DROP TABLE idempotency_records;
DROP TABLE audit_events;
DROP TABLE admin_operation_items;
DROP TABLE admin_operations;
DROP TABLE payment_observations;
DROP TABLE jobs;
DROP TABLE order_locations;
DROP TABLE orders;
DROP TABLE auth_flows;
DROP TABLE sessions;
DROP TABLE identities;
DROP TABLE users;
