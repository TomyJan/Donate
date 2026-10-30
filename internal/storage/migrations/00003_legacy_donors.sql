-- +goose Up
CREATE TABLE orders_next (
 id BLOB PRIMARY KEY CHECK(length(id)=16), user_id BLOB REFERENCES users(id), merchant_order_no TEXT NOT NULL UNIQUE,
 amount_fen INTEGER NOT NULL CHECK(amount_fen BETWEEN 1 AND 10000000), channel TEXT NOT NULL CHECK(channel IN ('alipay','wxpay') OR (protocol='legacy' AND channel='qqpay')),
 message TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','paid','closed')),
 close_reason TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, paid_at INTEGER, closed_at INTEGER,
 protocol TEXT NOT NULL DEFAULT 'v2', merchant_id TEXT NOT NULL DEFAULT '', gateway_id TEXT NOT NULL DEFAULT '',
 trade_no TEXT, api_trade_no TEXT NOT NULL DEFAULT '', gateway_status TEXT NOT NULL DEFAULT 'unknown',
 last_checked_at INTEGER, remote_close_status TEXT NOT NULL DEFAULT 'unknown', remote_closed_at INTEGER,
 hidden_order INTEGER NOT NULL DEFAULT 0 CHECK(hidden_order IN (0,1)), hidden_message INTEGER NOT NULL DEFAULT 0 CHECK(hidden_message IN (0,1)),
 request_ip TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, submission_status TEXT NOT NULL DEFAULT 'new',
 checkout_url TEXT NOT NULL DEFAULT '', submission_lease TEXT, submission_lease_until INTEGER,
 donor_nickname TEXT NOT NULL DEFAULT '', donor_avatar_url TEXT NOT NULL DEFAULT '', donor_qq_number TEXT NOT NULL DEFAULT '',
 CHECK(status!='paid' OR protocol='legacy' OR (paid_at IS NOT NULL AND trade_no IS NOT NULL)), UNIQUE(gateway_id,merchant_id,trade_no)
);
INSERT INTO orders_next(id,user_id,merchant_order_no,amount_fen,channel,message,status,close_reason,created_at,expires_at,paid_at,closed_at,protocol,merchant_id,gateway_id,trade_no,api_trade_no,gateway_status,last_checked_at,remote_close_status,remote_closed_at,hidden_order,hidden_message,request_ip,version,submission_status,checkout_url,submission_lease,submission_lease_until) SELECT id,user_id,merchant_order_no,amount_fen,channel,message,status,close_reason,created_at,expires_at,paid_at,closed_at,protocol,merchant_id,gateway_id,trade_no,api_trade_no,gateway_status,last_checked_at,remote_close_status,remote_closed_at,hidden_order,hidden_message,request_ip,version,submission_status,checkout_url,submission_lease,submission_lease_until FROM orders;
DROP TABLE orders;
ALTER TABLE orders_next RENAME TO orders;
CREATE INDEX orders_public ON orders(status,hidden_order,paid_at DESC,id DESC);
CREATE INDEX orders_user ON orders(user_id,created_at DESC,id DESC);
CREATE INDEX orders_created ON orders(created_at DESC,id DESC);
CREATE INDEX orders_channel ON orders(channel,status,created_at DESC);

-- +goose Down
CREATE TEMP TABLE legacy_downgrade_guard(n INTEGER CHECK(n=0));
INSERT INTO legacy_downgrade_guard SELECT count(*) FROM orders WHERE protocol='legacy' OR donor_nickname!='' OR donor_avatar_url!='' OR donor_qq_number!='';
DROP TABLE legacy_downgrade_guard;
CREATE TABLE orders_previous (
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
INSERT INTO orders_previous(id,user_id,merchant_order_no,amount_fen,channel,message,status,close_reason,created_at,expires_at,paid_at,closed_at,protocol,merchant_id,gateway_id,trade_no,api_trade_no,gateway_status,last_checked_at,remote_close_status,remote_closed_at,hidden_order,hidden_message,request_ip,version,submission_status,checkout_url,submission_lease,submission_lease_until) SELECT id,user_id,merchant_order_no,amount_fen,channel,message,status,close_reason,created_at,expires_at,paid_at,closed_at,protocol,merchant_id,gateway_id,trade_no,api_trade_no,gateway_status,last_checked_at,remote_close_status,remote_closed_at,hidden_order,hidden_message,request_ip,version,submission_status,checkout_url,submission_lease,submission_lease_until FROM orders;
DROP TABLE orders;
ALTER TABLE orders_previous RENAME TO orders;
CREATE INDEX orders_public ON orders(status,hidden_order,paid_at DESC,id DESC);
CREATE INDEX orders_user ON orders(user_id,created_at DESC,id DESC);
CREATE INDEX orders_created ON orders(created_at DESC,id DESC);
CREATE INDEX orders_channel ON orders(channel,status,created_at DESC);
