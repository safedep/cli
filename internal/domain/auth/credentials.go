package auth

import (
	"github.com/safedep/cli/internal/config"
	"github.com/safedep/dry/cloud"
)

// Saver persists API key credentials and updates the config tenant.
// Both auth login and setup mcp use this to avoid duplication.
type Saver struct {
	Store  cloud.CredentialStore
	Config *config.Config
}

func (s *Saver) Save(apiKey, tenant string) error {
	if err := s.Store.SaveAPIKeyCredential(apiKey, tenant); err != nil {
		return err
	}
	s.Config.Tenant = tenant
	return config.Save(s.Config)
}

func (s *Saver) Clear() error {
	return s.Store.Clear()
}
