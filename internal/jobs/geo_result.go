package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/TomyJan/Donate/internal/geo"
	"github.com/TomyJan/Donate/internal/order"
	"github.com/TomyJan/Donate/internal/storage"
)

func (r *Runner) saveGeoResult(ctx context.Context, j Job, o order.Order, p geo.Location, saveLocation, refresh bool) error {
	// A shutdown during lookup must retain diagnostics without exhausting the
	// retry budget. Bound this final write and still validate its lease in SQL.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if p.QueriedAt.IsZero() {
		p.QueriedAt = r.Queue.Now().UTC()
	}
	if p.Verification == "" {
		p.Verification = "single"
		if p.Country == "" {
			p.Verification = "unavailable"
		} else if p.Province == "" {
			p.Verification = "incomplete"
		}
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	oid, _ := storage.ParseID(o.ID)
	jid, _ := storage.ParseID(j.ID)
	return r.Queue.Store.Write(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM jobs WHERE id=? AND order_id=? AND kind='geo' AND source=? AND lease_token=? AND status='running' AND lease_until>?", jid, oid, j.Source, j.LeaseToken, r.Queue.Now().Unix()).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return order.ErrBusy
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO order_location_checks(order_id,result_json) VALUES(?,?) ON CONFLICT(order_id) DO UPDATE SET result_json=excluded.result_json", oid, string(raw)); err != nil {
			return err
		}
		if !saveLocation {
			return nil
		}
		conflict := "DO NOTHING"
		if refresh {
			conflict = "DO UPDATE SET country=excluded.country,province=excluded.province,city=excluded.city,district=excluded.district,street=excluded.street,isp=excluded.isp,asn=excluded.asn,latitude=excluded.latitude,longitude=excluded.longitude,accuracy_radius=excluded.accuracy_radius,source=excluded.source,source_version=excluded.source_version,queried_at=excluded.queried_at"
		}
		lid := storage.NewID()
		_, err := tx.ExecContext(ctx, "INSERT INTO order_locations(id,order_id,country,province,city,district,street,isp,asn,latitude,longitude,accuracy_radius,source,source_version,queried_at) SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM orders WHERE id=?) ON CONFLICT(order_id) "+conflict, lid[:], oid, p.Country, p.Province, p.City, p.District, p.Street, p.ISP, p.ASN, p.Latitude, p.Longitude, p.AccuracyRadius, p.Source, p.Version, p.QueriedAt.Unix(), oid)
		return err
	})
}
