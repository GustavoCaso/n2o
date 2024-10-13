package config

import "path/filepath"

type Config struct {
	Token                   string
	DatabaseID              string
	PageID                  string
	PagePropertiesToMigrate map[string]bool
	VaultPath               string
	VaultDestination        string
	StoreImages             bool
	PageNameFilters         map[string]string
	SaveToDisk              bool
}

func (c *Config) VaultFilepath() string {
	return filepath.Join(c.VaultPath, c.VaultDestination)
}

func (c *Config) VaultImagePath() string {
	return filepath.Join(c.VaultPath, "Images")
}
