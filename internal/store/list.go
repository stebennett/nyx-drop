package store

import (
	"context"
	"strings"
)

// ListParams selects a page of sites. An empty Query disables filtering.
// Sort accepts name, created or expires; Order accepts asc or desc.
type ListParams struct {
	Page    int // >= 1
	PerPage int // 1..200
	Sort    string
	Order   string
	Query   string
}

const literalSearch = `id LIKE '%' || ? || '%' ESCAPE '\'`

var escapeLike = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// List returns the requested page and the total number of matching sites.
func (d *DB) List(ctx context.Context, p ListParams) ([]Site, int, error) {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PerPage < 1 || p.PerPage > 200 {
		p.PerPage = 50
	}

	order := " DESC"
	if p.Order == "asc" {
		order = " ASC"
	}
	var sort string
	switch p.Sort {
	case "name":
		sort = "id" + order
	case "expires":
		sort = "permanent ASC, expires_at" + order + ", id ASC"
	default:
		sort = "created_at" + order + ", id ASC"
	}

	var where string
	var args []any
	if p.Query != "" {
		where = " WHERE " + literalSearch
		args = append(args, escapeLike.Replace(p.Query))
	}

	// Count and page must describe the same snapshot, even if other calls
	// concurrently create or delete sites.
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()

	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sites`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	items := []Site{}
	// Discard out-of-range pages before multiplication to avoid overflow.
	if total > 0 && p.Page-1 <= (total-1)/p.PerPage {
		rows, err := tx.QueryContext(ctx, `SELECT `+siteColumns+` FROM sites`+where+` ORDER BY `+sort+` LIMIT ? OFFSET ?`,
			append(args, p.PerPage, (p.Page-1)*p.PerPage)...)
		if err != nil {
			return nil, 0, err
		}
		defer rows.Close()
		for rows.Next() {
			s, err := scanSite(rows)
			if err != nil {
				return nil, 0, err
			}
			items = append(items, s)
		}
		if err := rows.Err(); err != nil {
			return nil, 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
