-- REALWORLD_SQL_SCHEMA inventory table for line_window chunking tests.

CREATE TABLE inventory (
    id SERIAL PRIMARY KEY,
    sku VARCHAR(64) NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_inventory_sku ON inventory (sku);
