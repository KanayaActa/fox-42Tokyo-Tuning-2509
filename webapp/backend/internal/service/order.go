package service

import (
	"backend/internal/model"
	"backend/internal/repository"
	"backend/internal/service/utils"
	"context"
	"crypto/md5"
	"fmt"
	"sync"
	"time"
)

// キャッシュエントリ
type CacheEntry struct {
	Data      interface{}
	ExpiresAt time.Time
}

// シンプルなインメモリキャッシュ
type SimpleCache struct {
	mu    sync.RWMutex
	items map[string]CacheEntry
}

func NewSimpleCache() *SimpleCache {
	cache := &SimpleCache{
		items: make(map[string]CacheEntry),
	}
	// 定期的に期限切れアイテムを削除
	go cache.cleanup()
	return cache
}

func (c *SimpleCache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	
	entry, exists := c.items[key]
	if !exists || time.Now().After(entry.ExpiresAt) {
		return nil, false
	}
	return entry.Data, true
}

func (c *SimpleCache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	c.items[key] = CacheEntry{
		Data:      value,
		ExpiresAt: time.Now().Add(ttl),
	}
}

func (c *SimpleCache) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, entry := range c.items {
			if now.After(entry.ExpiresAt) {
				delete(c.items, key)
			}
		}
		c.mu.Unlock()
	}
}

type OrderService struct {
	store *repository.Store
	cache *SimpleCache
}

func NewOrderService(store *repository.Store) *OrderService {
	return &OrderService{
		store: store,
		cache: NewSimpleCache(),
	}
}

// キャッシュキーを生成
func (s *OrderService) generateCacheKey(userID int, req model.ListRequest) string {
	key := fmt.Sprintf("orders:%d:%s:%s:%s:%d:%d", 
		userID, req.Search, req.Type, req.SortField, req.SortOrder, req.PageSize)
	hash := md5.Sum([]byte(key))
	return fmt.Sprintf("%x", hash)
}

// ユーザーの注文履歴を取得（キャッシュ付き）
func (s *OrderService) FetchOrders(ctx context.Context, userID int, req model.ListRequest) ([]model.Order, int, error) {
	// キャッシュキーを生成
	cacheKey := s.generateCacheKey(userID, req)
	
	// キャッシュから取得を試行
	if cached, found := s.cache.Get(cacheKey); found {
		if result, ok := cached.(struct {
			Orders []model.Order
			Total  int
		}); ok {
			return result.Orders, result.Total, nil
		}
	}

	var orders []model.Order
	var total int
	err := utils.WithTimeout(ctx, func(ctx context.Context) error {
		var fetchErr error
		orders, total, fetchErr = s.store.OrderRepo.ListOrders(ctx, userID, req)
		if fetchErr != nil {
			return fetchErr
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}

	// 結果をキャッシュに保存（30秒間）
	s.cache.Set(cacheKey, struct {
		Orders []model.Order
		Total  int
	}{Orders: orders, Total: total}, 30*time.Second)

	return orders, total, nil
}
