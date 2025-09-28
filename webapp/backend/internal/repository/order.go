package repository

import (
	"backend/internal/cache"
	"backend/internal/model"
	"context"
	"crypto/md5"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type OrderRepository struct {
	db    DBTX
	cache *cache.MemoryCache
}

func NewOrderRepository(db DBTX) *OrderRepository {
	return &OrderRepository{
		db:    db,
		cache: cache.NewMemoryCache(),
	}
}

// 複数の注文を一括作成し、生成された注文IDのスライスを返す
func (r *OrderRepository) CreateBatch(ctx context.Context, orders []*model.Order) ([]string, error) {
	if len(orders) == 0 {
		return []string{}, nil
	}

	// バッチINSERT用のクエリを構築
	query := `INSERT INTO orders (user_id, product_id, shipped_status, created_at) VALUES `
	placeholders := make([]string, len(orders))
	args := make([]interface{}, 0, len(orders)*4)

	for i, order := range orders {
		placeholders[i] = "(?, ?, 'shipping', NOW())"
		args = append(args, order.UserID, order.ProductID)
	}

	query += strings.Join(placeholders, ", ")

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}

	// 最初のIDを取得
	firstID, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	// 連続するIDのスライスを生成
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}

	orderIDs := make([]string, rowsAffected)
	for i := int64(0); i < rowsAffected; i++ {
		orderIDs[i] = fmt.Sprintf("%d", firstID+i)
	}

	// 注文が作成されたのでキャッシュを無効化
	r.invalidateOrderCountCache()

	return orderIDs, nil
}

// 注文を作成し、生成された注文IDを返す（単一注文用）
func (r *OrderRepository) Create(ctx context.Context, order *model.Order) (string, error) {
	// 単一注文をバッチメソッドで処理
	orderIDs, err := r.CreateBatch(ctx, []*model.Order{order})
	if err != nil {
		return "", err
	}
	if len(orderIDs) == 0 {
		return "", fmt.Errorf("no order ID generated")
	}
	return orderIDs[0], nil
}

// 複数の注文IDのステータスを一括で更新
// 主に配送ロボットが注文を引き受けた際に一括更新をするために使用
func (r *OrderRepository) UpdateStatuses(ctx context.Context, orderIDs []int64, newStatus string) error {
	if len(orderIDs) == 0 {
		return nil
	}
	query, args, err := sqlx.In("UPDATE orders SET shipped_status = ? WHERE order_id IN (?)", newStatus, orderIDs)
	if err != nil {
		return err
	}
	query = r.db.Rebind(query)
	_, err = r.db.ExecContext(ctx, query, args...)
	
	// ステータスが更新されたのでキャッシュを無効化
	if err == nil {
		r.invalidateOrderCountCache()
	}
	
	return err
}

// 配送中(shipped_status:shipping)の注文一覧を取得
func (r *OrderRepository) GetShippingOrders(ctx context.Context) ([]model.Order, error) {
	var orders []model.Order
	query := `
        SELECT
            o.order_id,
            p.weight,
            p.value
        FROM orders o
        JOIN products p ON o.product_id = p.product_id
        WHERE o.shipped_status = 'shipping'
    `
	err := r.db.SelectContext(ctx, &orders, query)
	return orders, err
}

// 注文履歴一覧を取得
func (r *OrderRepository) ListOrders(ctx context.Context, userID int, req model.ListRequest) ([]model.Order, int, error) {
	// キャッシュキーを生成（ユーザーIDと検索条件に基づく）
	cacheKey := r.generateOrderCountCacheKey(userID, req.Search, req.Type)
	
	// キャッシュから総件数を試行
	var total int
	if cachedTotal, found := r.cache.Get(cacheKey); found {
		total = cachedTotal.(int)
	} else {
		// キャッシュにない場合はDBから取得
		countQuery := `
	        SELECT COUNT(*)
	        FROM orders o
	        JOIN products p ON o.product_id = p.product_id
	        WHERE o.user_id = ?`
		
		countArgs := []interface{}{userID}
		
		// 検索条件があれば追加
		if req.Search != "" {
			if req.Type == "prefix" {
				countQuery += " AND p.name LIKE ?"
				countArgs = append(countArgs, req.Search+"%")
			} else {
				countQuery += " AND p.name LIKE ?"
				countArgs = append(countArgs, "%"+req.Search+"%")
			}
		}

		
		if err := r.db.GetContext(ctx, &total, countQuery, countArgs...); err != nil {
			return nil, 0, err
		}
		
		// 結果をキャッシュに保存（注文データは頻繁に変更される可能性があるため5秒間有効）
		r.cache.Set(cacheKey, total, 5*time.Second)
	}

	// メインクエリ
	query := `
        SELECT o.order_id, o.product_id, p.name as product_name, o.shipped_status, o.created_at, o.arrived_at
        FROM orders o
        JOIN products p ON o.product_id = p.product_id
        WHERE o.user_id = ?`
	
	args := []interface{}{userID}
	
	// 検索条件があれば追加
	if req.Search != "" {
		if req.Type == "prefix" {
			query += " AND p.name LIKE ?"
			args = append(args, req.Search+"%")
		} else {
			query += " AND p.name LIKE ?"
			args = append(args, "%"+req.Search+"%")
		}
	}

	// ソート処理（インデックス最適化版）
	switch req.SortField {
	case "product_name":
		// idx_products_name_id インデックスを活用
		query += " ORDER BY p.name " + strings.ToUpper(req.SortOrder) + ", o.order_id ASC"
	case "created_at":
		// idx_orders_user_created インデックスを活用
		if strings.ToUpper(req.SortOrder) == "DESC" {
			query += " ORDER BY o.created_at DESC, o.order_id ASC"
		} else {
			// ASCの場合はfilesortが発生するが、キャッシュで高速化
			query += " ORDER BY o.created_at ASC, o.order_id ASC"
		}
	case "shipped_status":
		// idx_orders_user_status_created インデックスを活用
		query += " ORDER BY o.shipped_status " + strings.ToUpper(req.SortOrder) + ", o.created_at DESC, o.order_id ASC"
	case "arrived_at":
		query += " ORDER BY o.arrived_at " + strings.ToUpper(req.SortOrder) + ", o.order_id ASC"
	case "order_id":
		fallthrough
	default:
		query += " ORDER BY o.order_id " + strings.ToUpper(req.SortOrder)
	}

	// ページング処理（req.PageSizeとreq.Offsetを使用）
	query += " LIMIT ? OFFSET ?"
	args = append(args, req.PageSize, req.Offset)

	type orderRow struct {
		OrderID       int64        `db:"order_id"`
		ProductID     int          `db:"product_id"`
		ProductName   string       `db:"product_name"`
		ShippedStatus string       `db:"shipped_status"`
		CreatedAt     sql.NullTime `db:"created_at"`
		ArrivedAt     sql.NullTime `db:"arrived_at"`
	}
	var ordersRaw []orderRow
	if err := r.db.SelectContext(ctx, &ordersRaw, query, args...); err != nil {
		return nil, 0, err
	}

	var orders []model.Order
	for _, o := range ordersRaw {
		orders = append(orders, model.Order{
			OrderID:       o.OrderID,
			ProductID:     o.ProductID,
			ProductName:   o.ProductName,
			ShippedStatus: o.ShippedStatus,
			CreatedAt:     o.CreatedAt.Time,
			ArrivedAt:     o.ArrivedAt,
		})
	}

	return orders, total, nil
}

// 注文件数キャッシュキーを生成する
func (r *OrderRepository) generateOrderCountCacheKey(userID int, search, searchType string) string {
	if search == "" {
		return fmt.Sprintf("order_count:user:%d:all", userID)
	}
	
	// 検索条件をハッシュ化してキーに含める
	searchKey := fmt.Sprintf("%s:%s", searchType, search)
	hash := md5.Sum([]byte(searchKey))
	return fmt.Sprintf("order_count:user:%d:search:%x", userID, hash)
}

// 注文データが更新された際にキャッシュを無効化する
func (r *OrderRepository) invalidateOrderCountCache() {
	// 注文関連のキャッシュを削除
	// 実装を簡単にするため、今回はキャッシュ全体をクリア
	r.cache = cache.NewMemoryCache()
}

// 特定ユーザーの注文キャッシュのみを無効化する（より細かい制御が必要な場合）
func (r *OrderRepository) InvalidateUserOrderCountCache(userID int) {
	// より効率的な実装では、ユーザー別のキーのみを削除
	// 現在の実装では全体をクリアしているが、将来的に改善可能
	r.invalidateOrderCountCache()
}
