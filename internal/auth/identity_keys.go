package auth

import (
	"context"
	"database/sql"
)

type identityReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type identityMatch struct {
	id, userID                            []byte
	status, qqNumber, qqOpenID, qqUnionID string
}

func (s *Service) normalizeIdentity(p Identity) (Identity, error) {
	if (p.Provider != "qq" && p.Provider != "oidc") || p.Issuer == "" || p.Subject == "" {
		return p, ErrInvalid
	}
	if p.Provider == "qq" {
		if s.Config.QQuseUnionID {
			if p.QQOpenID == "" || p.QQUnionID == "" {
				return p, ErrInvalid
			}
			p.Subject = p.QQUnionID
		} else {
			if p.QQOpenID == "" {
				p.QQOpenID = p.Subject
			}
			p.Subject = p.QQOpenID
			p.QQUnionID = ""
		}
		if p.QQNumber == "" {
			return p, ErrInvalid
		}
	}
	return p, nil
}

// A verified OpenID can upgrade its existing binding to UnionID. If the two
// keys point at different rows, fail closed; never merge users or move a binding.
func lookupIdentity(ctx context.Context, db identityReader, p Identity) (identityMatch, error) {
	query := "SELECT i.id,i.user_id,u.status,i.qq_number,i.qq_openid,i.qq_unionid FROM identities i JOIN users u ON u.id=i.user_id WHERE i.provider=? AND i.issuer=? AND i.subject=?"
	args := []any{p.Provider, p.Issuer, p.Subject}
	if p.Provider == "qq" {
		query = "SELECT i.id,i.user_id,u.status,i.qq_number,i.qq_openid,i.qq_unionid FROM identities i JOIN users u ON u.id=i.user_id WHERE i.provider='qq' AND i.issuer=? AND (i.qq_openid=? OR (?!='' AND i.qq_unionid=?))"
		args = []any{p.Issuer, p.QQOpenID, p.QQUnionID, p.QQUnionID}
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return identityMatch{}, err
	}
	defer rows.Close()
	var result identityMatch
	count := 0
	for rows.Next() {
		var row identityMatch
		if err = rows.Scan(&row.id, &row.userID, &row.status, &row.qqNumber, &row.qqOpenID, &row.qqUnionID); err != nil {
			return identityMatch{}, err
		}
		count++
		if count > 1 || p.QQUnionID != "" && row.qqUnionID != "" && p.QQUnionID != row.qqUnionID {
			return identityMatch{}, ErrConflict
		}
		result = row
	}
	if err = rows.Err(); err != nil {
		return identityMatch{}, err
	}
	if count == 0 {
		return result, sql.ErrNoRows
	}
	return result, nil
}
