-- +goose Up
DROP INDEX orders_public;
CREATE INDEX orders_public ON orders(status,hidden_order,COALESCE(paid_at,created_at) DESC,id DESC);

-- +goose Down
DROP INDEX orders_public;
CREATE INDEX orders_public ON orders(status,hidden_order,paid_at DESC,id DESC);
