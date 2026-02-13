package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/vgy789/noemx21-bot/internal/database/db"
	"github.com/vgy789/noemx21-bot/internal/service"
)

type WebhookHandler struct {
	apiKeyService *service.ApiKeyService
	queries       db.Querier
	log           *slog.Logger
}

func NewWebhookHandler(apiKeyService *service.ApiKeyService, queries db.Querier, log *slog.Logger) *WebhookHandler {
	return &WebhookHandler{
		apiKeyService: apiKeyService,
		queries:       queries,
		log:           log,
	}
}

type RegisterRequest struct {
	ExternalID string `json:"external_id"`
	Login      string `json:"login"`
}

type RegisterResponse struct {
	Registered bool `json:"registered"`
}

func (h *WebhookHandler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Auth
	secret := r.Header.Get("X-Secret")
	if secret == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	_, valid, err := h.apiKeyService.ValidateApiKey(r.Context(), secret)
	if err != nil {
		h.log.Error("failed to validate api key", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if !valid {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// 2. Parse Body
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// 3. Logic: Check if external_id is linked to login
	// We assume Platform is Telegram for now, as it's the main usage
	userAccount, err := h.queries.GetUserAccountByExternalId(r.Context(), db.GetUserAccountByExternalIdParams{
		Platform:   db.EnumPlatformTelegram,
		ExternalID: req.ExternalID,
	})

	registered := false
	if err == nil {
		if strings.EqualFold(userAccount.S21Login, req.Login) {
			registered = true
		}
	} else if !strings.Contains(err.Error(), "no rows") {
		h.log.Error("failed to get user account", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// 4. Response
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(RegisterResponse{Registered: registered})
}
