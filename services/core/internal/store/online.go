package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const onlineWindow = `interval '90 seconds'`

type OnlineEntry struct {
	Key   string
	IP    string
	Since time.Time
}

type NodeRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type OnlineIP struct {
	IP    string    `json:"ip"`
	Since time.Time `json:"since"`
}

type PresenceNode struct {
	NodeID   int64      `json:"node_id"`
	NodeName string     `json:"node_name"`
	IPs      []OnlineIP `json:"ips"`
}

type Presence struct {
	Online   bool           `json:"online"`
	LastSeen *time.Time     `json:"last_seen"`
	LastNode *NodeRef       `json:"last_node"`
	Now      []PresenceNode `json:"now"`
}

type IPHistoryRow struct {
	NodeID    int64     `json:"node_id"`
	NodeName  string    `json:"node_name"`
	IP        string    `json:"ip"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
}

type NodeOnlineUser struct {
	UserID           int64      `json:"user_id"`
	TelegramID       *int64     `json:"telegram_id"`
	TelegramUsername *string    `json:"telegram_username"`
	TelegramName     *string    `json:"telegram_name"`
	IPs              []OnlineIP `json:"ips"`
}

func (s *Store) ReportOnline(ctx context.Context, nodeID int64, entries []OnlineEntry) error {
	keys := make([]string, len(entries))
	ips := make([]string, len(entries))
	since := make([]time.Time, len(entries))
	for i, e := range entries {
		keys[i], ips[i], since[i] = e.Key, e.IP, e.Since
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM online_now WHERE node_id = $1`, nodeID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO online_now (node_id, user_id, ip, since, seen_at)
			 SELECT $1, uk.user_id, e.ip, e.since, now()
			 FROM unnest($2::text[], $3::text[], $4::timestamptz[]) AS e(key, ip, since)
			 JOIN user_keys uk ON uk.uuid = e.key
			 ORDER BY uk.user_id, e.ip
			 ON CONFLICT (node_id, user_id, ip) DO UPDATE SET since = LEAST(online_now.since, EXCLUDED.since)`,
			nodeID, keys, ips, since); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO ip_history (user_id, node_id, ip, first_seen, last_seen)
			 SELECT user_id, node_id, ip, since, seen_at FROM online_now WHERE node_id = $1 ORDER BY user_id, ip
			 ON CONFLICT (user_id, node_id, ip) DO UPDATE
			 SET first_seen = LEAST(ip_history.first_seen, EXCLUDED.first_seen), last_seen = GREATEST(ip_history.last_seen, EXCLUDED.last_seen)`,
			nodeID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO user_presence (user_id, last_seen, last_node_id)
			 SELECT DISTINCT ON (user_id) user_id, seen_at, node_id FROM online_now WHERE node_id = $1 ORDER BY user_id
			 ON CONFLICT (user_id) DO UPDATE SET last_seen = EXCLUDED.last_seen, last_node_id = EXCLUDED.last_node_id`,
			nodeID)
		return err
	})
}

func (s *Store) UserPresence(ctx context.Context, userID int64) (Presence, error) {
	p := Presence{Now: []PresenceNode{}}
	var nodeID *int64
	var nodeName *string
	err := s.pool.QueryRow(ctx,
		`SELECT up.last_seen, up.last_node_id, n.name FROM user_presence up LEFT JOIN nodes n ON n.id = up.last_node_id WHERE up.user_id = $1`,
		userID).Scan(&p.LastSeen, &nodeID, &nodeName)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return p, err
	}
	if nodeID != nil && nodeName != nil {
		p.LastNode = &NodeRef{ID: *nodeID, Name: *nodeName}
	}
	rows, err := s.pool.Query(ctx,
		`SELECT o.node_id, n.name, o.ip, o.since FROM online_now o JOIN nodes n ON n.id = o.node_id
		 WHERE o.user_id = $1 AND o.seen_at > now() - `+onlineWindow+`
		 ORDER BY n.name, o.node_id, o.since, o.ip`, userID)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		var ip OnlineIP
		if err := rows.Scan(&id, &name, &ip.IP, &ip.Since); err != nil {
			return p, err
		}
		if n := len(p.Now); n == 0 || p.Now[n-1].NodeID != id {
			p.Now = append(p.Now, PresenceNode{NodeID: id, NodeName: name})
		}
		last := &p.Now[len(p.Now)-1]
		last.IPs = append(last.IPs, ip)
	}
	p.Online = len(p.Now) > 0
	return p, rows.Err()
}

func (s *Store) UserIPHistory(ctx context.Context, userID int64, limit, offset int) ([]IPHistoryRow, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM ip_history WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT h.node_id, n.name, h.ip, h.first_seen, h.last_seen FROM ip_history h JOIN nodes n ON n.id = h.node_id
		 WHERE h.user_id = $1 ORDER BY h.last_seen DESC, h.ip LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []IPHistoryRow{}
	for rows.Next() {
		var r IPHistoryRow
		if err := rows.Scan(&r.NodeID, &r.NodeName, &r.IP, &r.FirstSeen, &r.LastSeen); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func (s *Store) NodeOnline(ctx context.Context, nodeID int64) ([]NodeOnlineUser, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT o.user_id, u.telegram_id, tp.username, `+telegramNameSQL+`, o.ip, o.since
		 FROM online_now o JOIN users u ON u.id = o.user_id LEFT JOIN telegram_profiles tp ON tp.user_id = u.id
		 WHERE o.node_id = $1 AND o.seen_at > now() - `+onlineWindow+`
		 ORDER BY o.user_id, o.since, o.ip`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NodeOnlineUser{}
	for rows.Next() {
		var u NodeOnlineUser
		var ip OnlineIP
		if err := rows.Scan(&u.UserID, &u.TelegramID, &u.TelegramUsername, &u.TelegramName, &ip.IP, &ip.Since); err != nil {
			return nil, err
		}
		if n := len(out); n == 0 || out[n-1].UserID != u.UserID {
			out = append(out, u)
		}
		last := &out[len(out)-1]
		last.IPs = append(last.IPs, ip)
	}
	return out, rows.Err()
}

func (s *Store) OnlineCounts(ctx context.Context) (int, map[int64]int, error) {
	byNode := map[int64]int{}
	rows, err := s.pool.Query(ctx,
		`SELECT node_id, count(DISTINCT user_id) FROM online_now WHERE seen_at > now() - `+onlineWindow+` GROUP BY node_id`)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return 0, nil, err
		}
		byNode[id] = n
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}
	var total int
	err = s.pool.QueryRow(ctx, `SELECT count(DISTINCT user_id) FROM online_now WHERE seen_at > now() - `+onlineWindow).Scan(&total)
	return total, byNode, err
}

func (s *Store) CleanupOnline(ctx context.Context, historyCutoff time.Time) (int64, error) {
	if _, err := s.pool.Exec(ctx, `DELETE FROM online_now WHERE seen_at < now() - interval '1 day'`); err != nil {
		return 0, err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM ip_history WHERE last_seen < $1`, historyCutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
