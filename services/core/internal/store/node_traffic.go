package store

import (
	"context"
	"math"
)

const (
	nodeTrafficDays = 30
	nodeTopSize     = 10
)

type UpDown struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type DayTraffic struct {
	Day string `json:"day"`
	UpDown
}

type TopUser struct {
	UserID           int64   `json:"user_id"`
	TelegramID       *int64  `json:"telegram_id"`
	TelegramUsername *string `json:"telegram_username"`
	TelegramName     *string `json:"telegram_name"`
	Bytes            int64   `json:"bytes"`
}

type NodeTop struct {
	Today  []TopUser `json:"today"`
	Days7  []TopUser `json:"days_7"`
	Days30 []TopUser `json:"days_30"`
}

type NodeTraffic struct {
	Today  UpDown       `json:"today"`
	Days7  UpDown       `json:"days_7"`
	Days30 UpDown       `json:"days_30"`
	Daily  []DayTraffic `json:"daily"`
	Top    NodeTop      `json:"top"`
}

func addSat(a, b int64) int64 {
	if a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

func (u UpDown) plus(v UpDown) UpDown {
	return UpDown{Up: addSat(u.Up, v.Up), Down: addSat(u.Down, v.Down)}
}

func (s *Store) NodeTraffic(ctx context.Context, nodeID int64) (NodeTraffic, error) {
	out := NodeTraffic{
		Daily: make([]DayTraffic, 0, nodeTrafficDays),
		Top:   NodeTop{Today: []TopUser{}, Days7: []TopUser{}, Days30: []TopUser{}},
	}
	rows, err := s.pool.Query(ctx,
		`WITH today AS (SELECT (now() AT TIME ZONE $2)::date AS d),
		 days AS (SELECT (today.d - g)::date AS day FROM today, generate_series(0, $3::int - 1) g),
		 t AS (SELECT td.day, sum(td.bytes_up) AS up, sum(td.bytes_down) AS down FROM traffic_daily td, today
		       WHERE td.node_id = $1 AND td.day > today.d - $3::int GROUP BY td.day)
		 SELECT to_char(days.day, 'YYYY-MM-DD'),
		        LEAST(COALESCE(t.up, 0), 9223372036854775807)::bigint,
		        LEAST(COALESCE(t.down, 0), 9223372036854775807)::bigint
		 FROM days LEFT JOIN t ON t.day = days.day ORDER BY days.day`,
		nodeID, s.statsTZ, nodeTrafficDays)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var d DayTraffic
		if err := rows.Scan(&d.Day, &d.Up, &d.Down); err != nil {
			rows.Close()
			return out, err
		}
		out.Daily = append(out.Daily, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	for i, d := range out.Daily {
		ago := len(out.Daily) - 1 - i
		if ago == 0 {
			out.Today = out.Today.plus(d.UpDown)
		}
		if ago < 7 {
			out.Days7 = out.Days7.plus(d.UpDown)
		}
		out.Days30 = out.Days30.plus(d.UpDown)
	}

	rows, err = s.pool.Query(ctx,
		`WITH today AS (SELECT (now() AT TIME ZONE $2)::date AS d),
		 t AS (SELECT td.user_id, td.day, td.bytes_up::numeric + td.bytes_down AS b FROM traffic_daily td, today
		       WHERE td.node_id = $1 AND td.day > today.d - $3::int),
		 w AS (SELECT 'today' AS win, t.user_id, sum(t.b) AS b FROM t, today WHERE t.day = today.d GROUP BY t.user_id
		       UNION ALL SELECT 'days_7', t.user_id, sum(t.b) FROM t, today WHERE t.day > today.d - 7 GROUP BY t.user_id
		       UNION ALL SELECT 'days_30', t.user_id, sum(t.b) FROM t GROUP BY t.user_id),
		 r AS (SELECT w.win, w.user_id, w.b, row_number() OVER (PARTITION BY w.win ORDER BY w.b DESC, w.user_id) AS rn
		       FROM w WHERE w.b > 0)
		 SELECT r.win, r.user_id, u.telegram_id, tp.username, `+telegramNameSQL+`,
		        LEAST(r.b, 9223372036854775807)::bigint
		 FROM r JOIN users u ON u.id = r.user_id LEFT JOIN telegram_profiles tp ON tp.user_id = r.user_id
		 WHERE r.rn <= $4 ORDER BY r.win, r.rn`,
		nodeID, s.statsTZ, nodeTrafficDays, nodeTopSize)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var win string
		var u TopUser
		if err := rows.Scan(&win, &u.UserID, &u.TelegramID, &u.TelegramUsername, &u.TelegramName, &u.Bytes); err != nil {
			return out, err
		}
		switch win {
		case "today":
			out.Top.Today = append(out.Top.Today, u)
		case "days_7":
			out.Top.Days7 = append(out.Top.Days7, u)
		case "days_30":
			out.Top.Days30 = append(out.Top.Days30, u)
		}
	}
	return out, rows.Err()
}
