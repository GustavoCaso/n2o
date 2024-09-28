package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVaultFilepath(t *testing.T) {
	c := Config{
		VaultPath:        "test",
		VaultDestination: "here",
	}

	assert.Equal(t, "test/here", c.VaultFilepath())
}

func TestVaultImagePath(t *testing.T) {
	c := Config{
		VaultPath:        "test",
		VaultDestination: "here",
	}

	assert.Equal(t, "test/Images", c.VaultImagePath())
}
