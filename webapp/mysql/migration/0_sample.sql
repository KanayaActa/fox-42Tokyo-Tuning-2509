-- このファイルに記述されたSQLコマンドが、マイグレーション時に実行されます。
-- 商品名のソート用インデックス
CREATE INDEX idx_products_name ON products(name);