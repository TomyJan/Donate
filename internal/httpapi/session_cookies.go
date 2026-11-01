package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/TomyJan/Donate/internal/auth"
	"github.com/TomyJan/Donate/internal/storage"
)

func (s *Server) sessionCookieName(id string) string { return s.sessionName() + "_" + id }
func (s *Server) browserCookieName(id string) string { return s.cookieName("browser") + "_" + id }

func (s *Server) setBrowserCookie(w http.ResponseWriter, family string, expiry time.Time) {
	if family != "" {
		s.setCookie(w, s.browserCookieName(family), s.Auth.BrowserProof(family), expiry)
	}
}

func (s *Server) writeSessionCookie(w http.ResponseWriter, r *http.Request, token string, session auth.Session) {
	s.setCookie(w, s.sessionCookieName(session.ID), token, session.ExpiresAt)
	s.setBrowserCookie(w, session.BrowserFamily, session.ExpiresAt)
	s.cleanSessionCookies(w, r, s.sessionCookieName(session.ID))
}

// Only exact names present on the initiating request may be removed. An old
// response cannot delete a newer session's independently named cookie.
func (s *Server) cleanSessionCookies(w http.ResponseWriter, r *http.Request, keep string) {
	seen := map[string]bool{}
	for _, cookie := range r.Cookies() {
		if seen[cookie.Name] || cookie.Name == keep {
			continue
		}
		seen[cookie.Name] = true
		if cookie.Name == s.sessionName() && keep == "" {
			continue
		}
		if cookie.Name == s.sessionName() || strings.HasPrefix(cookie.Name, s.sessionName()+"_") {
			s.setCookie(w, cookie.Name, "", time.Unix(0, 0))
		}
	}
}
func (s *Server) expireSessionCookie(w http.ResponseWriter, r *http.Request) {
	st := state(r)
	if st.Session != nil {
		s.setCookie(w, s.sessionCookieName(st.Session.ID), "", time.Unix(0, 0))
		s.setBrowserCookie(w, st.Session.BrowserFamily, s.Auth.Now().Add(auth.IdleTTL))
	}
	// Legacy deletion is safe: the new server never issues new credentials there.
	if cookie(r, s.sessionName()) != "" {
		s.setCookie(w, s.sessionName(), "", time.Unix(0, 0))
	}
}

func (s *Server) resolveSessionCookies(w http.ResponseWriter, r *http.Request) (*http.Request, error) {
	var candidates []auth.SessionCandidate
	var families []string
	var anchors []*http.Cookie
	var sessionCookies []*http.Cookie
	const cleanupLimit = 4 * auth.MaxBrowserCookies
	countSessions, countAnchors := 0, 0
	for _, cookie := range r.Cookies() {
		switch {
		case cookie.Name == s.sessionName():
			countSessions++
			if len(sessionCookies) < cleanupLimit {
				sessionCookies = append(sessionCookies, cookie)
			}
			if len(cookie.Value) == 43 && len(candidates) < cleanupLimit {
				candidates = append(candidates, auth.SessionCandidate{Token: cookie.Value})
			}
		case strings.HasPrefix(cookie.Name, s.sessionName()+"_"):
			countSessions++
			if len(sessionCookies) < cleanupLimit {
				sessionCookies = append(sessionCookies, cookie)
			}
			id := strings.TrimPrefix(cookie.Name, s.sessionName()+"_")
			bid, err := storage.ParseID(id)
			if err == nil && storage.IDString(bid) == id && len(cookie.Value) == 43 && len(candidates) < cleanupLimit {
				candidates = append(candidates, auth.SessionCandidate{ID: id, Token: cookie.Value})
			}
		case strings.HasPrefix(cookie.Name, s.cookieName("browser")+"_"):
			countAnchors++
			if len(anchors) < cleanupLimit {
				anchors = append(anchors, cookie)
			}
			id := strings.TrimPrefix(cookie.Name, s.cookieName("browser")+"_")
			if s.Auth.CheckBrowserProof(id, cookie.Value) && len(families) < cleanupLimit {
				families = append(families, id)
			}
		}
	}
	if countSessions > auth.MaxBrowserCookies || countAnchors > auth.MaxBrowserCookies {
		// Cleanup never turns this rejected request into an authenticated one.
		// A later retry presents the remaining complete set and is resolved anew.
		compacted, err := s.Auth.CompactBrowser(r.Context(), candidates, families)
		if err == nil {
			keep := ""
			if compacted.Token != "" {
				keep = s.sessionName()
				if compacted.CookieID != "" {
					keep = s.sessionCookieName(compacted.CookieID)
				}
			}
			for _, observed := range sessionCookies {
				if observed.Name != keep {
					s.setCookie(w, observed.Name, "", time.Unix(0, 0))
				}
			}
			for _, anchor := range anchors {
				if anchor.Name != s.browserCookieName(compacted.FamilyID) || !s.Auth.CheckBrowserProof(compacted.FamilyID, anchor.Value) {
					s.setCookie(w, anchor.Name, "", time.Unix(0, 0))
				}
			}
			s.setBrowserCookie(w, compacted.FamilyID, s.Auth.Now().Add(auth.IdleTTL))
		}
		return r, auth.ErrInvalid
	}
	resolved, err := s.Auth.ResolveBrowser(r.Context(), candidates, families)
	st := state(r)
	st.Token, st.Session, st.AuthError = resolved.Token, resolved.Session, err
	if resolved.FamilyID != "" {
		r = r.WithContext(auth.WithBrowserFamilies(r.Context(), []string{resolved.FamilyID}))
	}
	keep := ""
	if st.Session != nil {
		keep = s.sessionCookieName(st.Session.ID)
		if resolved.CookieID == "" {
			s.writeSessionCookie(w, r, st.Token, *st.Session)
		}
	}
	s.cleanSessionCookies(w, r, keep)
	canonicalAnchor := false
	for _, anchor := range anchors {
		if anchor.Name == s.browserCookieName(resolved.FamilyID) && s.Auth.CheckBrowserProof(resolved.FamilyID, anchor.Value) {
			canonicalAnchor = true
			continue
		}
		s.setCookie(w, anchor.Name, "", time.Unix(0, 0))
	}
	if !canonicalAnchor && resolved.FamilyID != "" && (st.Session != nil || len(families) > 0) {
		expiry := s.Auth.Now().Add(auth.IdleTTL)
		if st.Session != nil {
			expiry = st.Session.ExpiresAt
		}
		s.setBrowserCookie(w, resolved.FamilyID, expiry)
	}
	return r, nil
}
