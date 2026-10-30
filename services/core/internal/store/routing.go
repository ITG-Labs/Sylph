package store

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ITG-Labs/sylph/pkg/noderoute"
	"github.com/ITG-Labs/sylph/services/core/internal/model"
)

var (
	ErrStaleRevision = errors.New("stale revision")
	ErrProfileInUse  = errors.New("profile is in use")
)

type ProfileRow struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Doc       json.RawMessage `json:"doc"`
	Revision  int64           `json:"revision"`
	Nodes     int             `json:"nodes"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type NodeKey struct {
	UUID     string
	PlanID   int64
	EgressIP string
	Inbounds []string
}

type GeoSource struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	URL       string    `json:"url"`
	SHA256URL string    `json:"sha256_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

const profileColumns = `p.id, p.name, p.doc, p.revision, (SELECT count(*) FROM nodes n WHERE n.profile_id = p.id), p.created_at, p.updated_at`

func scanProfile(row pgxRow) (*ProfileRow, error) {
	var p ProfileRow
	err := row.Scan(&p.ID, &p.Name, &p.Doc, &p.Revision, &p.Nodes, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func nullJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func (s *Store) CreateProfile(ctx context.Context, name string, doc json.RawMessage) (*ProfileRow, error) {
	var id int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO routing_profiles (name, doc) VALUES ($1, $2) RETURNING id`, name, doc).Scan(&id); err != nil {
		return nil, mapWriteErr(err)
	}
	return s.GetProfile(ctx, id)
}

func (s *Store) GetProfile(ctx context.Context, id int64) (*ProfileRow, error) {
	return scanProfile(s.pool.QueryRow(ctx, `SELECT `+profileColumns+` FROM routing_profiles p WHERE p.id = $1`, id))
}

func (s *Store) ListProfiles(ctx context.Context) ([]ProfileRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+profileColumns+` FROM routing_profiles p ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProfileRow{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (s *Store) UpdateProfile(ctx context.Context, id int64, name string, doc json.RawMessage, revision int64) (*ProfileRow, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var current int64
	err = tx.QueryRow(ctx, `SELECT revision FROM routing_profiles WHERE id = $1 FOR UPDATE`, id).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if current != revision {
		return nil, ErrStaleRevision
	}
	if _, err := tx.Exec(ctx,
		`UPDATE routing_profiles SET name = $2, doc = $3, revision = revision + 1, updated_at = now() WHERE id = $1`,
		id, name, doc); err != nil {
		return nil, mapWriteErr(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE nodes SET users_version = next_users_version(users_version) WHERE profile_id = $1`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetProfile(ctx, id)
}

func (s *Store) DeleteProfile(ctx context.Context, id int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var locked int64
	err = tx.QueryRow(ctx, `SELECT id FROM routing_profiles WHERE id = $1 FOR UPDATE`, id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var used bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM nodes WHERE profile_id = $1)`, id).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrProfileInUse
	}
	if _, err := tx.Exec(ctx, `DELETE FROM routing_profiles WHERE id = $1`, id); err != nil {
		if errors.Is(mapWriteErr(err), ErrInvalidReference) {
			return ErrProfileInUse
		}
		return err
	}
	return tx.Commit(ctx)
}

type linkPair struct{ from, to int64 }

func (s *Store) RefreshLinks(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('sylph_node_links'))`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT n.id, p.doc FROM nodes n JOIN routing_profiles p ON p.id = n.profile_id WHERE n.enabled`)
	if err != nil {
		return err
	}
	var desired []linkPair
	for rows.Next() {
		var from int64
		var doc json.RawMessage
		if err := rows.Scan(&from, &doc); err != nil {
			rows.Close()
			return err
		}
		var p noderoute.Profile
		if err := json.Unmarshal(doc, &p); err != nil {
			continue
		}
		for _, to := range p.CascadeNodes() {
			if to != from {
				desired = append(desired, linkPair{from, to})
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var existing []linkPair
	rows, err = tx.Query(ctx, `SELECT from_node_id, to_node_id FROM node_links`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var l linkPair
		if err := rows.Scan(&l.from, &l.to); err != nil {
			rows.Close()
			return err
		}
		existing = append(existing, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var touched []int64
	for _, l := range desired {
		if slices.Contains(existing, l) {
			continue
		}
		tag, err := tx.Exec(ctx,
			`INSERT INTO node_links (from_node_id, to_node_id, key_uuid)
			 SELECT $1, $2, $3 WHERE EXISTS (SELECT 1 FROM nodes WHERE id = $2)
			 ON CONFLICT DO NOTHING`,
			l.from, l.to, model.NewUUID())
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			touched = append(touched, l.from, l.to)
		}
	}
	for _, l := range existing {
		if slices.Contains(desired, l) {
			continue
		}
		if _, err := tx.Exec(ctx, `DELETE FROM node_links WHERE from_node_id = $1 AND to_node_id = $2`, l.from, l.to); err != nil {
			return err
		}
		touched = append(touched, l.from, l.to)
	}
	if len(touched) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE nodes SET users_version = next_users_version(users_version) WHERE id = ANY($1)`, touched); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) ServiceKeys(ctx context.Context, toNodeID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT key_uuid FROM node_links WHERE to_node_id = $1 ORDER BY key_uuid`, toNodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) LinkKey(ctx context.Context, from, to int64) (string, error) {
	var key string
	err := s.pool.QueryRow(ctx, `SELECT key_uuid FROM node_links WHERE from_node_id = $1 AND to_node_id = $2`, from, to).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return key, err
}

func (s *Store) BumpNodes(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE nodes SET users_version = next_users_version(users_version) WHERE id = ANY($1)`, ids)
	return err
}

func (s *Store) BumpProfileNodes(ctx context.Context, profileIDs []int64) error {
	if len(profileIDs) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `UPDATE nodes SET users_version = next_users_version(users_version) WHERE profile_id = ANY($1)`, profileIDs)
	return err
}

func (s *Store) BumpNodesCascadingTo(ctx context.Context, nodeID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE nodes SET users_version = next_users_version(users_version)
		 WHERE id IN (SELECT from_node_id FROM node_links WHERE to_node_id = $1)`, nodeID)
	return err
}

func (s *Store) ListNodeKeys(ctx context.Context, nodeID int64) ([]NodeKey, error) {
	return s.listNodeKeys(ctx, nodeID, false)
}

func (s *Store) ListNodeKeysLegacy(ctx context.Context, nodeID int64) ([]NodeKey, error) {
	return s.listNodeKeys(ctx, nodeID, true)
}

func (s *Store) listNodeKeys(ctx context.Context, nodeID int64, everyInbound bool) ([]NodeKey, error) {
	rows, err := s.pool.Query(ctx,
		`WITH pi AS MATERIALIZED (SELECT plan_id, inbound_id, pool FROM plan_inbounds WHERE node_id = $1),
		 cur AS MATERIALIZED (
		     SELECT DISTINCT ON (sub.user_id) sub.id, sub.user_id, sub.plan_id
		     FROM subscriptions sub
		     WHERE NOT sub.canceled AND sub.starts_at <= now() AND sub.expires_at > now()
		     ORDER BY sub.user_id, sub.starts_at DESC, sub.id DESC),
		 spent AS MATERIALIZED (
		     SELECT x.period_id, COALESCE(x.pool_id, 0) AS pool
		     FROM cur JOIN period_pools x ON x.period_id = cur.id
		     WHERE x.limit_bytes IS NOT NULL AND x.used_bytes >= x.limit_bytes),
		 open AS (
		     SELECT cur.user_id, cur.plan_id, pi.inbound_id
		     FROM cur JOIN pi ON pi.plan_id = cur.plan_id
		     WHERE NOT EXISTS (SELECT 1 FROM spent WHERE spent.period_id = cur.id AND spent.pool = pi.pool))
		 SELECT uk.uuid, o.plan_id, COALESCE(na.ip, ''), array_agg(i.tag ORDER BY i.tag)
		 FROM open o
		 JOIN node_inbounds i ON i.id = o.inbound_id
		 JOIN users u ON u.id = o.user_id
		 JOIN user_keys uk ON uk.user_id = u.id
		 JOIN plans p ON p.id = o.plan_id
		 LEFT JOIN address_assignments aa ON aa.user_id = u.id AND aa.node_id = $1 AND p.dedicated_ip
		 LEFT JOIN node_addresses na ON na.id = aa.address_id AND na.status = 'ok'
		 WHERE u.status = 'active' AND uk.revoked_at IS NULL
		 GROUP BY uk.uuid, o.plan_id, na.ip
		 HAVING NOT $2 OR count(*) = (SELECT count(*) FROM node_inbounds WHERE node_id = $1)
		 ORDER BY uk.uuid`,
		nodeID, everyInbound)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []NodeKey
	for rows.Next() {
		var k NodeKey
		if err := rows.Scan(&k.UUID, &k.PlanID, &k.EgressIP, &k.Inbounds); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

const geoColumns = `id, name, kind, url, sha256_url, created_at, updated_at`

func scanGeo(row pgxRow) (*GeoSource, error) {
	var g GeoSource
	err := row.Scan(&g.ID, &g.Name, &g.Kind, &g.URL, &g.SHA256URL, &g.CreatedAt, &g.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, mapWriteErr(err)
	}
	return &g, nil
}

func (s *Store) CreateGeoSource(ctx context.Context, g GeoSource) (*GeoSource, error) {
	return scanGeo(s.pool.QueryRow(ctx,
		`INSERT INTO geo_sources (name, kind, url, sha256_url) VALUES ($1, $2, $3, $4) RETURNING `+geoColumns,
		g.Name, g.Kind, g.URL, g.SHA256URL))
}

func (s *Store) GetGeoSource(ctx context.Context, id int64) (*GeoSource, error) {
	return scanGeo(s.pool.QueryRow(ctx, `SELECT `+geoColumns+` FROM geo_sources WHERE id = $1`, id))
}

func (s *Store) UpdateGeoSource(ctx context.Context, g GeoSource) (*GeoSource, error) {
	return scanGeo(s.pool.QueryRow(ctx,
		`UPDATE geo_sources SET name = $2, kind = $3, url = $4, sha256_url = $5, updated_at = now() WHERE id = $1 RETURNING `+geoColumns,
		g.ID, g.Name, g.Kind, g.URL, g.SHA256URL))
}

func (s *Store) DeleteGeoSource(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM geo_sources WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListGeoSources(ctx context.Context) ([]GeoSource, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+geoColumns+` FROM geo_sources ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GeoSource{}
	for rows.Next() {
		g, err := scanGeo(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

func (s *Store) RequestGeoRefresh(ctx context.Context) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var gen int64
	if err := tx.QueryRow(ctx, `UPDATE settings SET geo_generation = geo_generation + 1 RETURNING geo_generation`).Scan(&gen); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE nodes SET users_version = next_users_version(users_version) WHERE profile_id IS NOT NULL`); err != nil {
		return 0, err
	}
	return gen, tx.Commit(ctx)
}
