package cache

import (
	"sync"
	"time"
)

type Cache[T any] struct {
	data map[string]cacheValue[T]
	lock sync.Mutex
}

type cacheValue[T any] struct {
	value      *T
	expiration time.Time
}

func NewCache[T any]() *Cache[T] {
	return &Cache[T]{
		data: make(map[string]cacheValue[T]),
	}
}

func (c *Cache[T]) Set(key string, value *T, expiration time.Duration) {
	c.lock.Lock()
	defer c.lock.Unlock()

	expirationTime := time.Now().Add(expiration)
	c.data[key] = cacheValue[T]{
		value:      value,
		expiration: expirationTime,
	}
}

func (c *Cache[T]) Get(key string) (*T, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	value, ok := c.data[key]
	if !ok || time.Now().After(value.expiration) {
		delete(c.data, key)

		return nil, false
	}

	return value.value, true
}
