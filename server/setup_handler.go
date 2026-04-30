// Copyright (c) 2026 BVK Chaitanya

package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"

	"github.com/bvk/tradebot/api"
)

func (s *Server) doSetup(ctx context.Context, req *api.SetupRequest) (*api.SetupResponse, error) {
	if err := req.Check(); err != nil {
		return nil, err
	}

	isUpdate := (req.Coinbase != nil ||
		req.CoinEx != nil ||
		req.Telegram != nil ||
		req.Pushover != nil)

	secrets, err := SecretsFromFile(s.secretsFilePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if !isUpdate {
			return &api.SetupResponse{}, nil
		}
	}
	if secrets == nil {
		secrets = new(Secrets)
	}

	resp := &api.SetupResponse{
		Coinbase: secrets.Coinbase,
		CoinEx:   secrets.CoinEx,
		Telegram: secrets.Telegram,
		Pushover: secrets.Pushover,
	}

	if req.Coinbase != nil {
		secrets.Coinbase = req.Coinbase
	}
	if req.CoinEx != nil {
		secrets.CoinEx = req.CoinEx
	}
	if req.Telegram != nil {
		secrets.Telegram = req.Telegram
	}
	if req.Pushover != nil {
		secrets.Pushover = req.Pushover
	}

	if isUpdate {
		js, err := json.MarshalIndent(secrets, "", "  ")
		if err != nil {
			return nil, err
		}
		// FIXME: We should write to temp file and do atomic rename.
		if err := os.WriteFile(s.secretsFilePath, js, os.FileMode(0600)); err != nil {
			return nil, err
		}
		slog.Info("updated the secrets file due to setup request")
	}
	return resp, nil
}
