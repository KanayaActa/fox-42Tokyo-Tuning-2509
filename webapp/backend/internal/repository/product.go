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

// 商品一覧を取得し、SQLでページング処理を行う
func (r *ProductRepository) ListProducts(ctx context.Context, userID int, req model.ListRequest) ([]model.Product, int, error) {
	var products []model.Product
	
	// まず総件数を取得
	countQuery := "SELECT COUNT(*) FROM products"
	countArgs := []interface{}{}
	
	if req.Search != "" {
		countQuery += " WHERE (name LIKE ? OR description LIKE ?)"
		searchPattern := req.Search + "%"
		countArgs = append(countArgs, searchPattern, searchPattern)
	}
	
	var total int
	err := r.db.GetContext(ctx, &total, countQuery, countArgs...)
	if err != nil {
		return nil, 0, err
	}
	
	// データを取得（プレースホルダーを使用してSQLインジェクションを防止）
	baseQuery := `
		SELECT product_id, name, value, weight, image, description
		FROM products
	`
	args := []interface{}{}

	if req.Search != "" {
		baseQuery += " WHERE (name LIKE ? OR description LIKE ?)"
		searchPattern := req.Search + "%"
		args = append(args, searchPattern, searchPattern)
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
