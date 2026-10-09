package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func (s *Server) registerServiceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/users", s.handleUserCreate)
	mux.HandleFunc("GET /v1/users/{telegram_id}", s.handleUserGet)
	mux.HandleFunc("GET /v1/users/by-token/{sub_token}", s.handleUserByToken)
	mux.HandleFunc("POST /v1/payments", s.handlePayment)
	mux.HandleFunc("POST /v1/users/{user_id}/rotate", s.handleRotate)
	mux.HandleFunc("POST /v1/users/{user_id}/devices/reset", s.handleDevicesReset)
	mux.HandleFunc("POST /v1/devices", s.handleDeviceRegister)
	mux.HandleFunc("POST /v1/maintenance/expire", s.handleMaintenanceExpire)
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
	u, key, err := s.svc.RegisterUser(r.Context(), body.TelegramID)
	if err != nil {
		s.log.Error("user_create_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "create failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": u.ID, "sub_token": u.SubToken, "key_uuid": key})
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
	u, err := s.svc.GetUserByTelegramIDTest(r.Context(), tg)
	if err != nil {
		s.log.Error("user_get_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "get failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "subscription": sub, "plan": plan})
}

func (s *Server) handleUserByToken(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("sub_token")
	u, keys, sub, plan, err := s.svc.GetUserFullByToken(r.Context(), token)
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
	key, err := s.svc.RotateUserKey(r.Context(), userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		s.log.Error("rotate_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "rotate failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key_uuid": key})
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

func (s *Server) handleDeviceRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SubToken    string `json:"sub_token"`
		Fingerprint string `json:"fingerprint"`
		UserAgent   string `json:"user_agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if body.SubToken == "" || body.Fingerprint == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "sub_token and fingerprint are required")
		return
	}
	allowed, err := s.svc.RegisterDevice(r.Context(), body.SubToken, body.Fingerprint, body.UserAgent)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		s.log.Error("device_register_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "register failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"allowed": allowed})
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
