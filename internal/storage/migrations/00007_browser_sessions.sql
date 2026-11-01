-- +goose Up
CREATE TABLE browser_families (
 id BLOB PRIMARY KEY CHECK(length(id)=16),
 parent_id BLOB REFERENCES browser_families(id),
 current_session_id BLOB,
 current_issued_order INTEGER NOT NULL DEFAULT 0
);
ALTER TABLE sessions ADD COLUMN browser_family_id BLOB REFERENCES browser_families(id);
ALTER TABLE sessions ADD COLUMN issued_order INTEGER NOT NULL DEFAULT 0;
ALTER TABLE auth_flows ADD COLUMN browser_family_id BLOB REFERENCES browser_families(id);
-- Existing credentials remain separate browsers until their verified cookies
-- establish a shared lineage. Persist the watermark independently of retention.
INSERT INTO browser_families(id,current_session_id,current_issued_order) SELECT id,id,rowid FROM sessions;
UPDATE sessions SET browser_family_id=id,issued_order=rowid;
UPDATE auth_flows SET browser_family_id=(SELECT browser_family_id FROM sessions WHERE sessions.id=auth_flows.session_id);
CREATE INDEX sessions_browser_family ON sessions(browser_family_id);
CREATE INDEX auth_flows_browser_family ON auth_flows(browser_family_id);
CREATE TABLE browser_session_sequence (id INTEGER PRIMARY KEY CHECK(id=1), issued_order INTEGER NOT NULL);
INSERT INTO browser_session_sequence(id,issued_order) SELECT 1,COALESCE(MAX(issued_order),0) FROM sessions;

-- +goose Down
DROP TABLE browser_session_sequence;
DROP INDEX auth_flows_browser_family;
DROP INDEX sessions_browser_family;
ALTER TABLE auth_flows DROP COLUMN browser_family_id;
ALTER TABLE sessions DROP COLUMN issued_order;
ALTER TABLE sessions DROP COLUMN browser_family_id;
DROP TABLE browser_families;
