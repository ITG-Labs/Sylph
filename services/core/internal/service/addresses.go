package service

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const maxAddressNoteRunes = 200

func (s *Service) ListAddresses(ctx context.Context) ([]store.NodeAddress, error) {
	list, err := s.store.ListAddresses(ctx)
	if err != nil {
		return nil, err
	}
	warn := map[int64]bool{}
	for i := range list {
		id := list[i].NodeID
		if _, done := warn[id]; !done {
			if warn[id], err = s.balancesDirect(ctx, id); err != nil {
				return nil, err
			}
		}
		list[i].BalancerWarning = warn[id]
	}
	return list, nil
}

func (s *Service) balancesDirect(ctx context.Context, nodeID int64) (bool, error) {
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil || node.ProfileID == nil {
		return false, err
	}
	row, err := s.store.GetProfile(ctx, *node.ProfileID)
	if err != nil {
		return false, err
	}
	p, err := parseProfile(row.Doc)
	if err != nil {
		return false, err
	}
	return p.BalancesDirect(), nil
}

func (s *Service) AssignAddress(ctx context.Context, addressID, userID int64, note string) (*store.NodeAddress, error) {
	if userID <= 0 {
		return nil, fieldErr("user_id", "required")
	}
	if utf8.RuneCountInString(note) > maxAddressNoteRunes {
		return nil, fieldErr("note", "at most 200 characters")
	}
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fieldErr("user_id", "user not found")
		}
		return nil, err
	}
	switch err := s.store.AssignAddress(ctx, addressID, userID, note); {
	case errors.Is(err, store.ErrAddressTaken):
		return nil, &ConflictError{Field: "address", Message: "address is assigned to another user: unassign it first"}
	case errors.Is(err, store.ErrUserHasNodeAddress):
		return nil, &ConflictError{Field: "user_id", Message: "user already has an address on this node"}
	case errors.Is(err, store.ErrInvalidReference):
		return nil, fieldErr("user_id", "user not found")
	case err != nil:
		return nil, err
	}
	return s.store.GetAddress(ctx, addressID)
}

func (s *Service) UnassignAddress(ctx context.Context, addressID int64) error {
	return s.store.UnassignAddress(ctx, addressID)
}

func (s *Service) DeleteAddress(ctx context.Context, addressID int64) error {
	if err := s.store.DeleteAddress(ctx, addressID); errors.Is(err, store.ErrAddressPresent) {
		return &ConflictError{Field: "address", Message: "the node still reports this address: remove it on the node with sylph ip remove first"}
	} else if err != nil {
		return err
	}
	return nil
}

func (s *Service) effectiveAddresses(ctx context.Context, userID int64) ([]DedicatedIP, error) {
	all, err := s.store.UserAddresses(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []DedicatedIP{}
	for _, a := range all {
		if a.Effective {
			out = append(out, DedicatedIP{IP: a.IP, NodeName: a.NodeName})
		}
	}
	return out, nil
}
