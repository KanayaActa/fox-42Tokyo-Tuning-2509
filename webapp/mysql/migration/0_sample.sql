-- このファイルに記述されたSQLコマンドが、マイグレーション時に実行されます。
CREATE INDEX idx_customer_lastname ON orders (order_id);
CREATE INDEX idx_customer_lastname ON orders (shipped_status);