-- +goose Up
ALTER TABLE identities ADD COLUMN qq_openid TEXT NOT NULL DEFAULT '';
ALTER TABLE identities ADD COLUMN qq_unionid TEXT NOT NULL DEFAULT '';
UPDATE identities SET qq_openid=subject WHERE provider='qq';
CREATE UNIQUE INDEX identities_qq_openid ON identities(issuer,qq_openid) WHERE provider='qq' AND qq_openid!='';
CREATE UNIQUE INDEX identities_qq_unionid ON identities(issuer,qq_unionid) WHERE provider='qq' AND qq_unionid!='';

-- +goose Down
-- Refuse a downgrade that would discard the OpenID lookup needed by existing bindings.
-- +goose StatementBegin
CREATE TEMP TABLE qq_downgrade_guard (n INTEGER CHECK(n=0));
-- +goose StatementEnd
INSERT INTO qq_downgrade_guard SELECT count(*) FROM identities WHERE qq_unionid!='';
DROP TABLE qq_downgrade_guard;
DROP INDEX identities_qq_unionid;
DROP INDEX identities_qq_openid;
ALTER TABLE identities DROP COLUMN qq_unionid;
ALTER TABLE identities DROP COLUMN qq_openid;
