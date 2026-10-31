-- +goose Up
-- The latest lookup is independent of the selected location snapshot: a failed
-- refresh keeps the previous valid province while retaining diagnostic evidence.
CREATE TABLE order_location_checks (
 order_id BLOB PRIMARY KEY REFERENCES orders(id) ON DELETE CASCADE CHECK(length(order_id)=16),
 result_json TEXT NOT NULL CHECK(json_valid(result_json))
);

-- +goose Down
DROP TABLE order_location_checks;
