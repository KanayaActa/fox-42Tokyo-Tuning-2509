package cache

import (
	"strings"
	"sync"
	"time"
)

// パターンベースでキーを削除する拡張版キャッシュ
type PatternMemoryCache struct {
	*MemoryCache
}

func NewPatternMemoryCache() *PatternMemoryCache {
	return &PatternMemoryCache{
		MemoryCache: NewMemoryCache(),
	}
}

// パターンにマッチするキーをすべて削除
func (c *PatternMemoryCache) DeleteByPattern(pattern string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	keysToDelete := make([]string, 0)
	for key := range c.items {
		if strings.Contains(key, pattern) {
			keysToDelete = append(keysToDelete, key)
		}
	}
	
	for _, key := range keysToDelete {
		delete(c.items, key)
	}
}