package service

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
	"github.com/ITG-Labs/sylph/services/core/internal/store"
)

const maxAddressNoteRunes = 200

func (s *Service) ListAddresses(ctx context.Context) ([]store.NodeAddress, error) {
	list, err := s.store.ListAddresses(ctx)
	if err != nil {
		return nil, err
	}
	profiles := map[int64]*noderoute.Profile{}
	for i := range list {
		p, err := s.addressProfile(ctx, profiles, list[i].NodeID)
		if err != nil {
			return nil, err
		}
		if p != nil {
			list[i].BalancerWarning = p.BalancesDirect()
			list[i].NoDirectWarning = !p.ReachesDirectAny()
		}
	}
	return list, nil
}

func (s *Service) addressProfile(ctx context.Context, cache map[int64]*noderoute.Profile, nodeID int64) (*noderoute.Profile, error) {
	if p, ok := cache[nodeID]; ok {
		return p, nil
	}
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	var p *noderoute.Profile
	if node.ProfileID != nil {
		row, err := s.store.GetProfile(ctx, *node.ProfileID)
		if err != nil {
			return nil, err
		}
		if p, err = parseProfile(row.Doc); err != nil {
			return nil, err
		}
	}
	cache[nodeID] = p
	return p, nil
}

func (s *Service) checkDirect(ctx context.Context, list []store.UserAddress) error {
	profiles := map[int64]*noderoute.Profile{}
	for i := range list {
		a := &list[i]
		if !a.Effective || a.PlanID == nil {
			continue
		}
		p, err := s.addressProfile(ctx, profiles, a.NodeID)
		if err != nil {
			return err
		}
		if p != nil && !p.ReachesDirect(*a.PlanID) {
			a.Effective, a.Reason = false, store.AddressReasonNoDirect
		}
	}
	return nil
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
	if err := s.checkDirect(ctx, all); err != nil {
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
