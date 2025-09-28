package cache

import (
	"os"
	"strconv"
	"time"
)

// キャッシュ設定を環境変数から取得
func GetCacheConfig() (productCacheDuration, orderCacheDuration time.Duration) {
	// デフォルト値
	productCacheDuration = 10 * time.Second
	orderCacheDuration = 5 * time.Second
	
	// 環境変数から設定を読み込み
	if val := os.Getenv("PRODUCT_CACHE_DURATION_SECONDS"); val != "" {
		if seconds, err := strconv.Atoi(val); err == nil && seconds > 0 {
			productCacheDuration = time.Duration(seconds) * time.Second
		}
	}
	
	if val := os.Getenv("ORDER_CACHE_DURATION_SECONDS"); val != "" {
		if seconds, err := strconv.Atoi(val); err == nil && seconds > 0 {
			orderCacheDuration = time.Duration(seconds) * time.Second
		}
	}
	
	return productCacheDuration, orderCacheDuration
}