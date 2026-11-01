package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/TomyJan/Donate/internal/storage"
)

const MaxBrowserCookies = 16

type browserContextKey struct{}

// WithBrowserFamilies carries only IDs whose browser-anchor MAC was verified.
// Each mutation resolves aliases again inside its own transaction.
func WithBrowserFamilies(ctx context.Context, families []string) context.Context {
	return context.WithValue(ctx, browserContextKey{}, append([]string(nil), families...))
}
func browserFamilies(ctx context.Context) []string {
	families, _ := ctx.Value(browserContextKey{}).([]string)
	return append([]string(nil), families...)
}

// BrowserProof associates a browser lineage; it cannot authenticate a user or
// retrieve a current bearer token. A separate, request-carried token is required.
func (s *Service) BrowserProof(id string) string {
	h := hmac.New(sha256.New, s.Config.Secret)
	h.Write([]byte("donate/browser-family/v1:" + id))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (s *Service) CheckBrowserProof(id, proof string) bool {
	bid, err := storage.ParseID(id)
	return err == nil && storage.IDString(bid) == id && len(proof) == 43 && hmac.Equal([]byte(s.BrowserProof(id)), []byte(proof))
}

type browserFamily struct {
	id, current []byte
	issued      int64
}
type browserReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func familyRoot(ctx context.Context, db browserReader, id []byte) (browserFamily, error) {
	// This loop follows only server-written aliases, never client-provided order.
	seen := map[string]bool{}
	for {
		if seen[string(id)] {
			return browserFamily{}, errors.New("cyclic browser family")
		}
		seen[string(id)] = true
		var out browserFamily
		var parent []byte
		err := db.QueryRowContext(ctx, "SELECT id,parent_id,current_session_id,current_issued_order FROM browser_families WHERE id=?", id).Scan(&out.id, &parent, &out.current, &out.issued)
		if err != nil {
			return browserFamily{}, err
		}
		if len(parent) == 0 {
			return out, nil
		}
		id = parent
	}
}

func (s *Service) mergeFamiliesTx(ctx context.Context, tx *sql.Tx, ids []string, create bool) (browserFamily, error) {
	roots := map[string]browserFamily{}
	var winner browserFamily
	for _, id := range ids {
		bid, err := storage.ParseID(id)
		if err != nil {
			continue
		}
		root, err := familyRoot(ctx, tx, bid)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return browserFamily{}, err
		}
		roots[string(root.id)] = root
		if winner.id == nil || root.issued > winner.issued || root.issued == winner.issued && string(root.id) < string(winner.id) {
			winner = root
		}
	}
	if winner.id == nil && create {
		id := storage.NewID()
		winner.id = id[:]
		_, err := tx.ExecContext(ctx, "INSERT INTO browser_families(id) VALUES(?)", winner.id)
		return winner, err
	}
	for _, root := range roots {
		if string(root.id) == string(winner.id) {
			continue
		}
		if _, err := tx.ExecContext(ctx, "UPDATE browser_families SET parent_id=? WHERE id=?", winner.id, root.id); err != nil {
			return browserFamily{}, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE sessions SET browser_family_id=? WHERE browser_family_id=?", winner.id, root.id); err != nil {
			return browserFamily{}, err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE auth_flows SET browser_family_id=? WHERE browser_family_id=?", winner.id, root.id); err != nil {
			return browserFamily{}, err
		}
	}
	if len(roots) > 1 {
		// Even an expired/revoked/deleted current keeps its issuance watermark.
		if _, err := tx.ExecContext(ctx, "UPDATE sessions SET revoked_at=COALESCE(revoked_at,?) WHERE browser_family_id=? AND id<>?", s.Now().Unix(), winner.id, winner.current); err != nil {
			return browserFamily{}, err
		}
	}
	return winner, nil
}

func currentBrowserSession(ctx context.Context, db browserReader, family, sid []byte) (bool, error) {
	root, err := familyRoot(ctx, db, family)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && string(root.current) == string(sid), err
}

type SessionCandidate struct {
	// ID is empty only for the legacy fixed-name cookie.
	ID, Token string
}
type BrowserResolution struct {
	FamilyID, Token, CookieID string
	Session                   *Session
}

// ResolveBrowser joins verified lineages, then authenticates only a bearer
// actually present in this request and matching the persistent current session.
func (s *Service) ResolveBrowser(ctx context.Context, candidates []SessionCandidate, families []string) (BrowserResolution, error) {
	return s.resolveBrowser(ctx, candidates, families, MaxBrowserCookies, true)
}

// CompactBrowser performs bounded metadata cleanup only. In particular it must
// never authenticate the best candidate from a truncated oversized request.
func (s *Service) CompactBrowser(ctx context.Context, candidates []SessionCandidate, families []string) (BrowserResolution, error) {
	return s.resolveBrowser(ctx, candidates, families, 4*MaxBrowserCookies, false)
}

func (s *Service) resolveBrowser(ctx context.Context, candidates []SessionCandidate, families []string, limit int, authenticate bool) (BrowserResolution, error) {
	var out BrowserResolution
	if len(candidates) > limit || len(families) > limit {
		return out, ErrInvalid
	}
	if len(candidates) == 0 && len(families) == 0 {
		return out, nil
	}
	var chosen SessionCandidate
	err := s.Store.Write(ctx, func(tx *sql.Tx) error {
		ids := append([]string(nil), families...)
		type knownSession struct{ id, family []byte }
		known := map[string]knownSession{}
		if len(candidates) > 0 {
			args := make([]any, len(candidates))
			for i, candidate := range candidates {
				args[i] = hash(candidate.Token)
			}
			rows, err := tx.QueryContext(ctx, "SELECT token_hash,id,browser_family_id FROM sessions WHERE token_hash IN ("+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+")", args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var tokenHash []byte
				var record knownSession
				if err := rows.Scan(&tokenHash, &record.id, &record.family); err != nil {
					rows.Close()
					return err
				}
				known[string(tokenHash)] = record
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
			for _, candidate := range candidates {
				record, ok := known[string(hash(candidate.Token))]
				if ok && (candidate.ID == "" || candidate.ID == storage.IDString(record.id)) {
					ids = append(ids, storage.IDString(record.family))
				}
			}
		}
		root, err := s.mergeFamiliesTx(ctx, tx, ids, false)
		if err != nil {
			return err
		}
		out.FamilyID = storage.IDString(root.id)
		for _, candidate := range candidates {
			record, ok := known[string(hash(candidate.Token))]
			if ok && string(record.id) == string(root.current) && (candidate.ID == "" || candidate.ID == storage.IDString(record.id)) {
				chosen = candidate
				break
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	if !authenticate {
		out.Token, out.CookieID = chosen.Token, chosen.ID
		return out, nil
	}
	if chosen.Token == "" {
		if len(candidates) > 0 {
			return out, ErrUnauthorized
		}
		return out, nil
	}
	session, err := s.Resolve(ctx, chosen.Token)
	if err != nil {
		return out, err
	}
	out.Token, out.CookieID, out.Session = chosen.Token, chosen.ID, &session
	return out, nil
}
