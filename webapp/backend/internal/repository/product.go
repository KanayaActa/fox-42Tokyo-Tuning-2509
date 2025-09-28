package repository

import (
	"backend/internal/cache"
	"backend/internal/model"
	"context"
	"crypto/md5"
	"fmt"
	"time"
)

type ProductRepository struct {
	db    DBTX
	cache *cache.MemoryCache
}

func NewProductRepository(db DBTX) *ProductRepository {
	return &ProductRepository{
		db:    db,
		cache: cache.NewMemoryCache(),
	}
}

// 商品一覧を取得し、SQLでページング処理を行う
func (r *ProductRepository) ListProducts(ctx context.Context, userID int, req model.ListRequest) ([]model.Product, int, error) {
	var products []model.Product
	
	// キャッシュキーを生成（検索条件に基づく）
	cacheKey := r.generateCountCacheKey(req.Search)
	
	// キャッシュから総件数を試行
	var total int
	if cachedTotal, found := r.cache.Get(cacheKey); found {
		total = cachedTotal.(int)
	} else {
		// キャッシュにない場合はDBから取得
		countQuery := "SELECT COUNT(*) FROM products"
		countArgs := []interface{}{}
		
		if req.Search != "" {
			countQuery += " WHERE (name LIKE ? OR description LIKE ?)"
			searchPattern := "%" + req.Search + "%"
			countArgs = append(countArgs, searchPattern, searchPattern)
		}
		
		err := r.db.GetContext(ctx, &total, countQuery, countArgs...)
		if err != nil {
			return nil, 0, err
		}
		
		// 結果をキャッシュに保存（10秒間有効）
		r.cache.Set(cacheKey, total, 10*time.Second)
	}
	
	// データを取得（プレースホルダーを使用してSQLインジェクションを防止）
	baseQuery := `
		SELECT product_id, name, value, weight, image, description
		FROM products
	`
	args := []interface{}{}

	if req.Search != "" {
		baseQuery += " WHERE (name LIKE ? OR description LIKE ?)"
		searchPattern := "%" + req.Search + "%"
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

	err := r.db.SelectContext(ctx, &products, baseQuery, args...)
	if err != nil {
		return nil, 0, err
	}

	return products, total, nil
}

// キャッシュキーを生成する（検索条件に基づく）
func (r *ProductRepository) generateCountCacheKey(search string) string {
	if search == "" {
		return "product_count:all"
	}
	
	// 検索条件をハッシュ化してキーに含める
	hash := md5.Sum([]byte(search))
	return fmt.Sprintf("product_count:search:%x", hash)
}

// 商品データが更新された際にキャッシュを無効化する
func (r *ProductRepository) InvalidateCountCache() {
	// 全てのカウントキャッシュを削除
	// 実装を簡単にするため、今回はキャッシュ全体をクリア
	r.cache = cache.NewMemoryCache()
}
