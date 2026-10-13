package api

import (
	"context"
	"net/http"

	"github.com/ITG-Labs/sylph/pkg/mtls"
	"github.com/ITG-Labs/sylph/services/core/internal/service"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

func (s *Server) registerPaymentAdminRoutes(mux *http.ServeMux) {
	s.route(mux, "GET /v1/admin/payments", s.handleAdminPaymentList, mtls.RolePanel)
	s.route(mux, "GET /v1/admin/payments/{payment_id}", s.handleAdminPaymentGet, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/payments/{payment_id}/recheck", s.handleAdminPaymentRecheck, mtls.RolePanel)
	s.route(mux, "POST /v1/admin/payments/{payment_id}/apply", s.paymentAction("apply", s.svc.ApplyPayment), mtls.RolePanel)
	s.route(mux, "POST /v1/admin/payments/{payment_id}/reject", s.paymentAction("reject", s.svc.RejectPayment), mtls.RolePanel)
	s.route(mux, "POST /v1/admin/payments/{payment_id}/refund", s.paymentAction("refund", s.svc.RefundPayment), mtls.RolePanel)
}

func pathPaymentID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("payment_id")
	if !validPaymentID(id) {
		writeFieldError(w, http.StatusBadRequest, "bad_request", "payment_id", "must be 1-64 characters of A-Z, a-z, 0-9, '.', '_' and '-'")
		return "", false
	}
	return id, true
}

func (s *Server) handleAdminPaymentList(w http.ResponseWriter, r *http.Request) {
	q, ok := readQuery(w, r, "status", "source", "user_id", "stuck", "before", "limit")
	if !ok {
		return
	}
	query := service.PaymentQuery{
		Status: q.str("status"), Source: q.str("source"), UserID: q.optID("user_id"), Stuck: q.bool("stuck"),
		Before: q.int("before", 0, 1<<62, 0), Limit: int(q.int("limit", 1, 100, 50)),
	}
	if !q.ok() {
		return
	}
	rows, err := s.svc.ListPayments(r.Context(), query)
	if err != nil {
		s.writePaymentError(w, err, "payments")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"payments": rows, "next_before": nextBefore(rows, query.Limit, func(p store.PaymentRow) int64 { return p.ID })})
}

func (s *Server) handleAdminPaymentGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathPaymentID(w, r)
	if !ok {
		return
	}
	row, err := s.svc.GetPaymentRow(r.Context(), id)
	if err != nil {
		s.writePaymentError(w, err, "payment")
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) handleAdminPaymentRecheck(w http.ResponseWriter, r *http.Request) {
	id, ok := pathPaymentID(w, r)
	if !ok {
		return
	}
	res, err := s.svc.RecheckPayment(r.Context(), id)
	if err != nil {
		s.writePaymentError(w, err, "payment recheck")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) paymentAction(action string, do func(ctx context.Context, paymentID, reason string) (*service.PaymentActionResult, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathPaymentID(w, r)
		if !ok {
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		res, err := do(r.Context(), id, body.Reason)
		if err != nil {
			s.writePaymentError(w, err, "payment "+action)
			return
		}
		s.log.Info("admin_change", "entity", "payment", "action", action, "payment_id", id, "peer", clientCN(r))
		writeJSON(w, http.StatusOK, res)
	}
}
