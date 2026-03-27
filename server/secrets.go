// Copyright (c) 2023 BVK Chaitanya

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/bvk/tradebot/api"
	"github.com/bvk/tradebot/coinbase"
	"github.com/bvk/tradebot/coinex"
	"github.com/bvk/tradebot/pushover"
	"github.com/bvk/tradebot/telegram"
)

type Secrets struct {
	Coinbase *coinbase.Credentials `json:"coinbase"`
	CoinEx   *coinex.Credentials   `json:"coinex"`
	Pushover *pushover.Keys        `json:"pushover"`
	Telegram *telegram.Secrets     `json:"telegram"`
}

func SecretsFromFile(fpath string) (*Secrets, error) {
	data, err := os.ReadFile(fpath)
	if err != nil {
		return nil, err
	}
	s := new(Secrets)
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	return s, nil
}

// SecretsToFile writes secrets to the given path as formatted JSON (mode 0600).
func SecretsToFile(fpath string, s *Secrets) error {
	if s == nil {
		s = new(Secrets)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fpath, data, 0600)
}

func (v *Secrets) Check() error {
	if v.Coinbase != nil {
		if err := v.Coinbase.Check(); err != nil {
			return fmt.Errorf("Coinbase: %w", err)
		}
	}
	if v.CoinEx != nil {
		if err := v.CoinEx.Check(); err != nil {
			return fmt.Errorf("CoinEx: %w", err)
		}
	}
	if v.Telegram != nil {
		if err := v.Telegram.Check(); err != nil {
			return fmt.Errorf("Telegram: %w", err)
		}
	}
	if v.Pushover != nil {
		if err := v.Pushover.Check(); err != nil {
			return fmt.Errorf("Pushover: %w", err)
		}
	}
	return nil
}

// SecretsValidationError is returned when settings POST has missing or invalid fields.
// The HTTP layer returns 400 with the error message.
type SecretsValidationError struct {
	Message string
}

func (e *SecretsValidationError) Error() string {
	return e.Message
}

// MaskPlaceholder is sent by the UI for "keep existing value"; backend does not overwrite.
const MaskPlaceholder = "••••••••"

func mask(s string) string {
	if s == "" {
		return ""
	}
	return MaskPlaceholder
}

func (s *Server) doSecretsGet(ctx context.Context) (*api.SecretsGetResponse, error) {
	// Load raw secrets without validation so the settings UI can open even when
	// the file is empty or only partially configured (LoadSecrets would return 500).
	secrets, err := SecretsFromFile(s.secretsFilePath)
	if err != nil {
		// Propagate so the HTTP wrapper can translate os.ErrNotExist into 404.
		return nil, err
	}
	if secrets == nil {
		secrets = new(Secrets)
	}
	resp := &api.SecretsGetResponse{
		Exchanges:     make(map[string]api.SecretsConfig),
		Notifications: make(map[string]api.SecretsConfig),
	}

	if secrets.Coinbase != nil {
		resp.Exchanges["coinbase"] = api.SecretsConfig{
			Enabled: true,
			Config: map[string]string{
				"kid": mask(secrets.Coinbase.KID),
				"pem": mask(secrets.Coinbase.PEM),
			},
		}
	} else {
		resp.Exchanges["coinbase"] = api.SecretsConfig{Enabled: false, Config: map[string]string{"kid": "", "pem": ""}}
	}

	if secrets.CoinEx != nil {
		resp.Exchanges["coinex"] = api.SecretsConfig{
			Enabled: true,
			Config: map[string]string{
				"key":    mask(secrets.CoinEx.Key),
				"secret": mask(secrets.CoinEx.Secret),
			},
		}
	} else {
		resp.Exchanges["coinex"] = api.SecretsConfig{Enabled: false, Config: map[string]string{"key": "", "secret": ""}}
	}

	if secrets.Telegram != nil {
		cfg := map[string]string{
			"token": mask(secrets.Telegram.BotToken),
			"owner": mask(secrets.Telegram.OwnerID),
			"admin": mask(secrets.Telegram.AdminID),
		}
		if len(secrets.Telegram.OtherIDs) > 0 {
			cfg["others"] = mask(strings.Join(secrets.Telegram.OtherIDs, ","))
		} else {
			cfg["others"] = ""
		}
		resp.Notifications["telegram"] = api.SecretsConfig{Enabled: true, Config: cfg}
	} else {
		resp.Notifications["telegram"] = api.SecretsConfig{Enabled: false, Config: map[string]string{"token": "", "owner": "", "admin": "", "others": ""}}
	}

	if secrets.Pushover != nil {
		resp.Notifications["pushover"] = api.SecretsConfig{
			Enabled: true,
			Config: map[string]string{
				"application_key": mask(secrets.Pushover.ApplicationKey),
				"user_key":        mask(secrets.Pushover.UserKey),
			},
		}
	} else {
		resp.Notifications["pushover"] = api.SecretsConfig{Enabled: false, Config: map[string]string{"application_key": "", "user_key": ""}}
	}

	return resp, nil
}

// keepOrNew returns current if newVal is the mask placeholder (unchanged);
// otherwise returns newVal, so empty string clears the value.
func keepOrNew(current, newVal string) string {
	if newVal == MaskPlaceholder {
		return current
	}
	return newVal
}

func (s *Server) doSecretsPost(ctx context.Context, req *api.SecretsPostRequest) (*api.SecretsPostResponse, error) {
	if s.secretsFilePath == "" {
		return nil, os.ErrNotExist
	}

	// Load existing secrets from file (no validation) so we merge on top of current state.
	curSecrets, err := SecretsFromFile(s.secretsFilePath)
	if err != nil {
		curSecrets = new(Secrets)
	}
	if curSecrets == nil {
		curSecrets = new(Secrets)
	}

	// Start with a copy of current secrets; we only overlay sections that appear in the request.
	out := &Secrets{
		Coinbase: curSecrets.Coinbase,
		CoinEx:   curSecrets.CoinEx,
		Telegram: curSecrets.Telegram,
		Pushover: curSecrets.Pushover,
	}

	if req.Exchanges != nil {
		if c, ok := req.Exchanges["coinbase"]; ok && c.Config != nil {
			if c.Enabled {
				cur := curSecrets.Coinbase
				if cur == nil {
					cur = &coinbase.Credentials{}
				}
				out.Coinbase = &coinbase.Credentials{
					KID: keepOrNew(cur.KID, c.Config["kid"]),
					PEM: keepOrNew(cur.PEM, c.Config["pem"]),
				}
			} else {
				out.Coinbase = nil
			}
		}
		if c, ok := req.Exchanges["coinex"]; ok && c.Config != nil {
			if c.Enabled {
				cur := curSecrets.CoinEx
				if cur == nil {
					cur = &coinex.Credentials{}
				}
				out.CoinEx = &coinex.Credentials{
					Key:    keepOrNew(cur.Key, c.Config["key"]),
					Secret: keepOrNew(cur.Secret, c.Config["secret"]),
				}
			} else {
				out.CoinEx = nil
			}
		}
	}

	if req.Notifications != nil {
		if c, ok := req.Notifications["telegram"]; ok && c.Config != nil {
			if c.Enabled {
				cur := curSecrets.Telegram
				if cur == nil {
					cur = &telegram.Secrets{}
				}
				othersStr := keepOrNew(strings.Join(cur.OtherIDs, ","), c.Config["others"])
				var others []string
				if othersStr != "" {
					others = strings.Split(othersStr, ",")
					for i := range others {
						others[i] = strings.TrimSpace(others[i])
					}
				}
				out.Telegram = &telegram.Secrets{
					BotToken: keepOrNew(cur.BotToken, c.Config["token"]),
					OwnerID:  keepOrNew(cur.OwnerID, c.Config["owner"]),
					AdminID:  keepOrNew(cur.AdminID, c.Config["admin"]),
					OtherIDs: others,
				}
			} else {
				out.Telegram = nil
			}
		}
		if c, ok := req.Notifications["pushover"]; ok && c.Config != nil {
			if c.Enabled {
				cur := curSecrets.Pushover
				if cur == nil {
					cur = &pushover.Keys{}
				}
				out.Pushover = &pushover.Keys{
					ApplicationKey: keepOrNew(cur.ApplicationKey, c.Config["application_key"]),
					UserKey:        keepOrNew(cur.UserKey, c.Config["user_key"]),
				}
			} else {
				out.Pushover = nil
			}
		}
	}

	if err := out.Check(); err != nil {
		return nil, &SecretsValidationError{Message: err.Error()}
	}

	if err := SecretsToFile(s.secretsFilePath, out); err != nil {
		return nil, err
	}
	return &api.SecretsPostResponse{OK: true}, nil
}
