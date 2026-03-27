// Copyright (c) 2025 BVK Chaitanya

package api

const SecretsPath = "/api/v1/secrets"

// SecretsConfig is the enabled flag and config fields for one exchange or notification.
type SecretsConfig struct {
	Enabled bool              `json:"enabled"`
	Config  map[string]string `json:"config"`
}

// SecretsGetResponse is the GET /api/v1/secrets response (config values masked).
type SecretsGetResponse struct {
	Exchanges    map[string]SecretsConfig `json:"exchanges"`
	Notifications map[string]SecretsConfig `json:"notifications"`
}

// SecretsPostRequest is the POST /api/v1/secrets body; config values may use a mask placeholder to keep existing.
type SecretsPostRequest struct {
	Exchanges    map[string]SecretsConfig `json:"exchanges"`
	Notifications map[string]SecretsConfig `json:"notifications"`
}

// SecretsPostResponse is the POST /api/v1/secrets success response.
type SecretsPostResponse struct {
	OK bool `json:"ok"`
}
