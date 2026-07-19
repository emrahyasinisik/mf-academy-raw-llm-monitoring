package config

import (
	"net/http"

	"github.com/emrah/mf-backend/internal/common"
)

// Handler serves public application configuration and metadata.
type Handler struct {
	cfg Config
}

func NewHandler(cfg Config) *Handler { return &Handler{cfg: cfg} }

// Config returns non-secret settings the frontend needs to configure itself.
// GET /config  — never expose secrets (JWT secret, DB URL) here.
func (h *Handler) Config(w http.ResponseWriter, r *http.Request) {
	common.JSON(w, http.StatusOK, map[string]any{
		"app_name":    h.cfg.AppName,
		"version":     h.cfg.AppVersion,
		"environment": h.cfg.Env,
		"features": map[string]bool{
			"registration":     true,
			"decision_scoring": true,
			"webllm":           true,
		},
		"scoring": map[string]any{
			"type":       "rule-based",
			"dimensions": []string{"completion", "latency", "efficiency", "keywords", "length"},
			"grades":     []string{"A", "B", "C", "D", "F"},
		},
	})
}

// Version returns just the build/version info. GET /version
func (h *Handler) Version(w http.ResponseWriter, r *http.Request) {
	common.JSON(w, http.StatusOK, map[string]string{
		"name":    h.cfg.AppName,
		"version": h.cfg.AppVersion,
	})
}
