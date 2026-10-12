package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func (s *Server) registerServiceRoutes(mux *http.ServeMux) {
	s.route(mux, "POST /v1/users", s.handleUserCreate, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "GET /v1/users/{telegram_id}", s.handleUserGet, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/users/by-token", s.handleUserByToken, mtls.RoleSubs, mtls.RolePanel)
	s.route(mux, "POST /v1/payments", s.handlePayment, mtls.RolePanel)
	s.route(mux, "POST /v1/users/{user_id}/rotate", s.handleRotate, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/users/{user_id}/devices/reset", s.handleDevicesReset, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/subscription", s.handleSubscription, mtls.RoleSubs, mtls.RolePanel)
	s.route(mux, "POST /v1/maintenance/expire", s.handleMaintenanceExpire, mtls.RoleCron, mtls.RolePanel)
	s.route(mux, "POST /v1/maintenance/cleanup", s.handleMaintenanceCleanup, mtls.RoleCron, mtls.RolePanel)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TelegramID int64 `json:"telegram_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if body.TelegramID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "telegram_id must be positive")
		return
	}
	u, _, err := s.svc.RegisterUser(r.Context(), body.TelegramID)
	if err != nil {
		s.log.Error("user_create_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "create failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "sub_token": u.SubToken})
}

func (s *Server) handleUserGet(w http.ResponseWriter, r *http.Request) {
	tg, err := strconv.ParseInt(r.PathValue("telegram_id"), 10, 64)
	if err != nil || tg <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid telegram_id")
		return
	}
	sub, plan, err := s.svc.GetUserSubscriptionInfo(r.Context(), tg)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		s.log.Error("user_get_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "get failed")
		return
	}
	u, err := s.svc.GetUserByTelegramID(r.Context(), tg)
	if err != nil {
		s.log.Error("user_get_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "get failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "subscription": sub, "plan": plan})
}

func (s *Server) handleUserByToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SubToken string `json:"sub_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if body.SubToken == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "sub_token is required")
		return
	}
	u, keys, sub, plan, err := s.svc.GetUserFullByToken(r.Context(), body.SubToken)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		s.log.Error("user_by_token_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "get failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "keys": keys, "subscription": sub, "plan": plan})
}

func (s *Server) handlePayment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PaymentID string `json:"payment_id"`
		UserID    int64  `json:"user_id"`
		PlanID    int64  `json:"plan_id"`
		AmountRub int64  `json:"amount_rub"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if body.PaymentID == "" || body.UserID <= 0 || body.PlanID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "payment_id, user_id and plan_id are required")
		return
	}
	err := s.svc.HandlePayment(r.Context(), body.PaymentID, body.UserID, body.PlanID, body.AmountRub)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user or plan not found")
			return
		}
		s.log.Error("payment_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "payment failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleRotate(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user_id")
		return
	}
	if _, err := s.svc.RotateUserKey(r.Context(), userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		s.log.Error("rotate_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "rotate failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDevicesReset(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user_id")
		return
	}
	if err := s.svc.ResetUserDevices(r.Context(), userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		s.log.Error("devices_reset_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "reset failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMaintenanceExpire(w http.ResponseWriter, r *http.Request) {
	n, err := s.svc.CheckAndExpire(r.Context(), time.Now().UTC())
	if err != nil {
		s.log.Error("expire_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "expire failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"expired": n})
}

func (s *Server) handleMaintenanceCleanup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		OlderThanDays *int `json:"older_than_days"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.OlderThanDays == nil {
		writeFieldError(w, http.StatusBadRequest, "bad_request", "older_than_days", "is required")
		return
	}
	res, err := s.svc.CleanupHistory(r.Context(), *body.OlderThanDays, time.Now().UTC())
	if err != nil {
		s.writeServiceError(w, err, "cleanup")
		return
	}
	writeJSON(w, http.StatusOK, res)
}
