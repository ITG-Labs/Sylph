package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func (s *Server) registerServiceRoutes(mux *http.ServeMux) {
	s.route(mux, "POST /v1/users", s.handleUserCreate, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "GET /v1/users/{telegram_id}", s.handleUserGet, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/users/by-token", s.handleUserByToken, mtls.RoleSubs, mtls.RolePanel)
	s.route(mux, "POST /v1/payments", s.handlePayment, mtls.RolePanel)
	s.route(mux, "POST /v1/payments/create", s.handlePaymentCreate, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/payments/{payment_id}/check", s.handlePaymentCheck, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "GET /v1/plans", s.handlePlansOnSale, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/users/{user_id}/rotate", s.handleRotate, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/users/{user_id}/devices/reset", s.handleDevicesReset, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "GET /v1/users/{user_id}/devices", s.handleDevicesList, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "DELETE /v1/users/{user_id}/devices/{device_id}", s.handleDeviceRemove, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/users/{user_id}/trial", s.handleTrial, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "GET /v1/users/{user_id}/stats", s.handleUserStats, mtls.RoleBot, mtls.RolePanel)
	s.route(mux, "POST /v1/subscription", s.handleSubscription, mtls.RoleSubs, mtls.RolePanel)
	s.route(mux, "POST /v1/maintenance/expire", s.handleMaintenanceExpire, mtls.RoleCron, mtls.RolePanel)
	s.route(mux, "POST /v1/maintenance/cleanup", s.handleMaintenanceCleanup, mtls.RoleCron, mtls.RolePanel)
	s.route(mux, "POST /v1/maintenance/payments/poll", s.handlePaymentsPoll, mtls.RoleCron, mtls.RolePanel)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TelegramID int64                         `json:"telegram_id"`
		Profile    *service.TelegramProfileInput `json:"profile"`
	}
	if !decodeBody(w, r, &body) {
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
	if body.Profile != nil {
		if err := s.svc.SaveTelegramProfile(r.Context(), u.ID, *body.Profile); err != nil {
			s.log.Error("telegram_profile_failed", "user_id", u.ID, "error", err.Error())
		}
	}
	if u.Status == model.UserBanned {
		writeBanned(w)
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
	st, err := s.svc.GetUserStatus(r.Context(), tg)
	if err != nil {
		if errors.Is(err, store.ErrUserBanned) {
			writeBanned(w)
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		s.log.Error("user_get_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "get failed")
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleUserByToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SubToken string `json:"sub_token"`
	}
	if !decodeBody(w, r, &body) {
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
	if !decodeBody(w, r, &body) {
		return
	}
	if body.PaymentID == "" || body.UserID <= 0 || body.PlanID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "payment_id, user_id and plan_id are required")
		return
	}
	res, err := s.svc.RecordPayment(r.Context(), body.PaymentID, body.UserID, body.PlanID, body.AmountRub)
	if err != nil {
		var ce *service.ConflictError
		switch {
		case errors.As(err, &ce):
			writeFieldError(w, http.StatusConflict, "conflict", ce.Field, ce.Message)
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "user or plan not found")
		default:
			s.log.Error("payment_failed", "error", err.Error())
			writeError(w, http.StatusInternalServerError, "internal", "payment failed")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "applied": res.Applied, "status": res.Payment.Status})
}

func (s *Server) handlePaymentCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID int64 `json:"user_id"`
		PlanID int64 `json:"plan_id"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.UserID <= 0 || body.PlanID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "user_id and plan_id are required")
		return
	}
	p, err := s.svc.CreatePayment(r.Context(), body.UserID, body.PlanID)
	if err != nil {
		s.writePaymentError(w, err, "payment create")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"payment_id": p.PaymentID, "confirmation_url": p.ConfirmationURL, "amount_rub": p.AmountRub})
}

func (s *Server) handlePaymentCheck(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("payment_id")
	if !validPaymentID(id) {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid payment_id")
		return
	}
	res, err := s.svc.CheckPayment(r.Context(), id)
	if err != nil {
		s.writePaymentError(w, err, "payment check")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handlePaymentsPoll(w http.ResponseWriter, r *http.Request) {
	res, err := s.svc.PollPayments(r.Context())
	if err != nil {
		s.writePaymentError(w, err, "payment poll")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handlePlansOnSale(w http.ResponseWriter, r *http.Request) {
	list, err := s.svc.ListPlansOnSale(r.Context())
	if err != nil {
		s.writeServiceError(w, err, "plans")
		return
	}
	writeJSON(w, http.StatusOK, nonNil(list))
}

func validPaymentID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		ok := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_' || c == '.'
		if !ok {
			return false
		}
	}
	return true
}

func (s *Server) writePaymentError(w http.ResponseWriter, err error, what string) {
	var ce *service.ConflictError
	var fe *service.FieldError
	switch {
	case errors.As(err, &fe):
		writeFieldError(w, http.StatusBadRequest, "bad_request", fe.Field, fe.Message)
	case errors.Is(err, service.ErrPaymentsDisabled):
		writeError(w, http.StatusServiceUnavailable, "payments_disabled", "payments are not configured")
	case errors.Is(err, service.ErrProviderUnavailable):
		writeError(w, http.StatusBadGateway, "provider_unavailable", "payment provider unavailable")
	case errors.As(err, &ce):
		writeFieldError(w, http.StatusConflict, "conflict", ce.Field, ce.Message)
	case errors.Is(err, store.ErrUserBanned):
		writeBanned(w)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "user, plan or payment not found")
	default:
		s.log.Error("payment_failed", "what", what, "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", what+" failed")
	}
}

func (s *Server) handleRotate(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user_id")
		return
	}
	token, err := s.svc.RotateUser(r.Context(), userID)
	if err != nil {
		if errors.Is(err, store.ErrUserBanned) {
			writeBanned(w)
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		s.log.Error("rotate_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "rotate failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"sub_token": token})
}

func (s *Server) handleDevicesReset(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user_id")
		return
	}
	if err := s.svc.ResetUserDevices(r.Context(), userID); err != nil {
		if errors.Is(err, store.ErrUserBanned) {
			writeBanned(w)
			return
		}
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

func (s *Server) handleDevicesList(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user_id")
		return
	}
	list, err := s.svc.ListUserDevices(r.Context(), userID)
	if err != nil {
		s.writeServiceError(w, err, "user")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDeviceRemove(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	deviceID, err2 := strconv.ParseInt(r.PathValue("device_id"), 10, 64)
	if err != nil || err2 != nil || userID <= 0 || deviceID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user_id or device_id")
		return
	}
	if err := s.svc.RemoveUserDevice(r.Context(), userID, deviceID); err != nil {
		s.writeServiceError(w, err, "device")
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
		OlderThanDays      *int `json:"older_than_days"`
		StatsOlderThanDays *int `json:"stats_older_than_days"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.OlderThanDays == nil {
		writeFieldError(w, http.StatusBadRequest, "bad_request", "older_than_days", "is required")
		return
	}
	res, err := s.svc.CleanupHistory(r.Context(), *body.OlderThanDays, body.StatsOlderThanDays, time.Now().UTC())
	if err != nil {
		s.writeServiceError(w, err, "cleanup")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleTrial(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user_id")
		return
	}
	res, err := s.svc.GrantTrial(r.Context(), userID)
	switch {
	case errors.Is(err, store.ErrTrialUnavailable):
		writeError(w, http.StatusConflict, "trial_unavailable", "trial is not available for this user")
	case errors.Is(err, store.ErrUserBanned):
		writeBanned(w)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "user not found")
	case err != nil:
		s.log.Error("trial_failed", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "trial failed")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"paid_until": res.ExpiresAt})
	}
}

func (s *Server) handleUserStats(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil || userID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid user_id")
		return
	}
	st, err := s.svc.UserStats(r.Context(), userID)
	if err != nil {
		s.writeServiceError(w, err, "user")
		return
	}
	writeJSON(w, http.StatusOK, st)
}
