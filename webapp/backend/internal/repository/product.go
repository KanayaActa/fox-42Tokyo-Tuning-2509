package repository

import (
	"backend/internal/model"
	"context"
)

type ProductRepository struct {
	db DBTX
}

func NewProductRepository(db DBTX) *ProductRepository {
	return &ProductRepository{db: db}
}

// 商品一覧を取得し、SQLでページング処理を行う（最適化版）
func (r *ProductRepository) ListProducts(ctx context.Context, userID int, req model.ListRequest) ([]model.Product, int, error) {
	var products []model.Product
	var total int
	var countQuery string
	var countArgs []interface{}
	var baseQuery string
	var args []interface{}

	// 検索条件に応じて最適化されたクエリを選択
	if req.Search != "" {
		// FULLTEXT検索を使用（nameとdescriptionの両方を検索）
		countQuery = `
			SELECT COUNT(*)
			FROM products
			WHERE MATCH(name, description) AGAINST (? IN BOOLEAN MODE)`
		countArgs = []interface{}{req.Search}

		baseQuery = `
			SELECT product_id, name, value, weight, image, description
			FROM products
			WHERE MATCH(name, description) AGAINST (? IN BOOLEAN MODE)`
		args = []interface{}{req.Search}
	} else {
		// 検索なし
		countQuery = "SELECT COUNT(*) FROM products"
		countArgs = []interface{}{}

		baseQuery = `
			SELECT product_id, name, value, weight, image, description
			FROM products`
		args = []interface{}{}
	}

	// 総件数を取得
	err := r.db.GetContext(ctx, &total, countQuery, countArgs...)
	if err != nil {
		return nil, 0, err
	}

	// ソートフィールドのバリデーション（SQLインジェクション防止）
	allowedSortFields := map[string]bool{
		"product_id":  true,
		"name":        true,
		"value":       true,
		"weight":      true,
		"description": true,
	}
	if !allowedSortFields[req.SortField] {
		req.SortField = "product_id"
	}
	
	// ソート順のバリデーション
	if req.SortOrder != "ASC" && req.SortOrder != "DESC" {
		req.SortOrder = "ASC"
	}

	baseQuery += " ORDER BY " + req.SortField + " " + req.SortOrder + ", product_id ASC LIMIT ? OFFSET ?"
	args = append(args, req.PageSize, req.Offset)

	err = r.db.SelectContext(ctx, &products, baseQuery, args...)
	if err != nil {
		return nil, 0, err
	}

	return products, total, nil
}
