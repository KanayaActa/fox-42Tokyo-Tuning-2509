package repository

import (
	"backend/internal/model"
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

type OrderRepository struct {
	db DBTX
}

func NewOrderRepository(db DBTX) *OrderRepository {
	return &OrderRepository{db: db}
}

// 注文を作成し、生成された注文IDを返す
func (r *OrderRepository) Create(ctx context.Context, order *model.Order) (string, error) {
	query := `INSERT INTO orders (user_id, product_id, shipped_status, created_at) VALUES (?, ?, 'shipping', NOW())`
	result, err := r.db.ExecContext(ctx, query, order.UserID, order.ProductID)
	if err != nil {
		return "", err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d", id), nil
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

// 注文履歴一覧を取得（最適化版）
func (r *OrderRepository) ListOrders(ctx context.Context, userID int, req model.ListRequest) ([]model.Order, int, error) {
	var total int
	var countQuery string
	var countArgs []interface{}
	var query string
	var args []interface{}

	// 検索条件に応じて最適化されたクエリを選択
	if req.Search != "" && req.Type != "prefix" {
		// 部分検索: FULLTEXT検索を使用
		countQuery = `
            SELECT COUNT(*)
            FROM orders o
            JOIN (
                SELECT product_id
                FROM products
                WHERE MATCH(name) AGAINST (? IN BOOLEAN MODE)
            ) p ON o.product_id = p.product_id
            WHERE o.user_id = ?`
		countArgs = []interface{}{req.Search, userID}

		query = `
            SELECT o.order_id, o.product_id, p2.name as product_name, o.shipped_status, o.created_at, o.arrived_at
            FROM orders o
            JOIN (
                SELECT product_id
                FROM products
                WHERE MATCH(name) AGAINST (? IN BOOLEAN MODE)
            ) p ON o.product_id = p.product_id
            JOIN products p2 ON p2.product_id = o.product_id
            WHERE o.user_id = ?`
		args = []interface{}{req.Search, userID}
	} else {
		// 前方一致検索または検索なし: 従来のLIKE検索
		countQuery = `
            SELECT COUNT(*)
            FROM orders o
            JOIN products p ON o.product_id = p.product_id
            WHERE o.user_id = ?`
		countArgs = []interface{}{userID}

		query = `
            SELECT o.order_id, o.product_id, p.name as product_name, o.shipped_status, o.created_at, o.arrived_at
            FROM orders o
            JOIN products p ON o.product_id = p.product_id
            WHERE o.user_id = ?`
		args = []interface{}{userID}

		// 前方一致検索条件を追加
		if req.Search != "" && req.Type == "prefix" {
			countQuery += " AND p.name LIKE ?"
			countArgs = append(countArgs, req.Search+"%")
			query += " AND p.name LIKE ?"
			args = append(args, req.Search+"%")
		}
	}

	// 総件数を取得
	err := r.db.GetContext(ctx, &total, countQuery, countArgs...)
	if err != nil {
		return nil, 0, err
	}

	// ソート処理（最適化版）
	switch req.SortField {
	case "product_name":
		if req.Search != "" && req.Type != "prefix" {
			// FULLTEXT検索の場合はp2.nameを使用
			query += " ORDER BY p2.name " + strings.ToUpper(req.SortOrder) + ", o.order_id ASC"
		} else {
			query += " ORDER BY p.name " + strings.ToUpper(req.SortOrder) + ", o.order_id ASC"
		}
	case "created_at":
		query += " ORDER BY o.created_at " + strings.ToUpper(req.SortOrder) + ", o.order_id ASC"
	case "shipped_status":
		query += " ORDER BY o.shipped_status " + strings.ToUpper(req.SortOrder) + ", o.order_id ASC"
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

