package cache

import "sync"

type Cache struct {
	storage map[string]string
	working map[string]bool
	mu      sync.RWMutex
}

func (c *Cache) Get(value string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.storage[value]
	return val, ok
}

func (c *Cache) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.storage[key] = value
	c.working[key] = false
}

func (c *Cache) Mark(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.working[key] = true
}

func (c *Cache) IsWorking(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.working[key]
}

func NewCache() *Cache {
	storage := map[string]string{}
	working := map[string]bool{}
	return &Cache{
		storage: storage,
		working: working,
	}
}
