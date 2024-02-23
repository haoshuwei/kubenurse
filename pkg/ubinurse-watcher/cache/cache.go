package cache

import (
	"github.com/patrickmn/go-cache"
)

type CacheManager struct {
	*cache.Cache
}

func NewCacheManager() *CacheManager {
	chanCache := cache.New(cache.NoExpiration, cache.NoExpiration)
	return &CacheManager{
		Cache: chanCache,
	}
}
