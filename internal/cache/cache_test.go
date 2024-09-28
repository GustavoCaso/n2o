package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCache(t *testing.T) {
	c := NewCache()

	key := "test"

	_, ok := c.Get(key)
	assert.False(t, ok)

	working := c.IsWorking(key)
	assert.False(t, working)

	c.Mark(key)

	working = c.IsWorking(key)
	assert.True(t, working)

	c.Set(key, "foo")

	val, ok := c.Get(key)
	assert.True(t, ok)
	assert.Equal(t, "foo", val)
}
