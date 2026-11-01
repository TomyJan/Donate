package geo

import (
	"context"
	"errors"
	"net/netip"
	"time"
)

// Evidence records an observed provider result, never an invented probability.
type Evidence struct {
	Source    string    `json:"source"`
	Version   string    `json:"version,omitempty"`
	Country   string    `json:"country,omitempty"`
	Province  string    `json:"province,omitempty"`
	City      string    `json:"city,omitempty"`
	Outcome   string    `json:"outcome"`
	QueriedAt time.Time `json:"queried_at"`
}

func evidence(p Location, source string, err error) Evidence {
	outcome := "complete"
	if err != nil || p.Country == "" {
		outcome = "unavailable"
	} else if p.Province == "" {
		outcome = "incomplete"
	}
	at := p.QueriedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return Evidence{Source: source, Version: p.Version, Country: p.Country, Province: p.Province, City: p.City, Outcome: outcome, QueriedAt: at}
}

func complete(p Location) bool        { return p.Country != "" && p.Province != "" }
func sameProvince(a, b Location) bool { return a.Country == b.Country && a.Province == b.Province }

func decision(p Location, status string, conflict bool, observations []Evidence) Location {
	p.Verification, p.Conflict = status, conflict
	p.Evidence = append([]Evidence(nil), observations...)
	return p
}

func cloneLocation(p Location) Location {
	p.Evidence = append([]Evidence(nil), p.Evidence...)
	if p.Latitude != nil {
		n := *p.Latitude
		p.Latitude = &n
	}
	if p.Longitude != nil {
		n := *p.Longitude
		p.Longitude = &n
	}
	if p.AccuracyRadius != nil {
		n := *p.AccuracyRadius
		p.AccuracyRadius = &n
	}
	return p
}

func (s *Service) resolve(ctx context.Context, a netip.Addr) (Location, error) {
	ip := a.String()
	var primary, partial Location
	var observations []Evidence
	var lastFailure error
	if s.config.GeoMode == "local" {
		local, err := s.lookupLocal(a)
		observations = append(observations, local.Evidence...)
		if complete(local) && err == nil {
			primary = local
		} else if local.Country != "" && err == nil {
			partial = local
		}
		if !s.config.GeoOnlineFallback && s.localReady(a) {
			if local.Country == "" && err == nil {
				err = ErrUnavailable
			}
			status := "single"
			if !complete(local) {
				status = "incomplete"
			}
			return decision(local, status, false, observations), err
		}
	}
	var challenger Location
	conflict := false
	for _, src := range s.sources {
		if src.v4only && !a.Is4() {
			continue
		}
		out, err := s.lookupOnline(ctx, src, ip)
		observations = append(observations, evidence(out, src.name, err))
		if err != nil {
			lastFailure = err
			continue
		}
		if !complete(out) {
			if partial.Country == "" {
				partial = out
			}
			continue
		}
		if !complete(primary) {
			return decision(out, "single", false, observations), nil
		}
		if sameProvince(primary, out) {
			return decision(primary, "corroborated", conflict, observations), nil
		}
		conflict = true
		if complete(challenger) {
			if sameProvince(challenger, out) {
				s.log.Warn("IP "+ip+" 的本地属地与两个在线来源不一致，采用 "+challenger.Source+" 的核验结果", "ip", ip, "evidence", observations)
				return decision(challenger, "corroborated", true, observations), nil
			}
			break // At most two complete online candidates for one local result.
		}
		challenger = out
	}
	if complete(primary) && !conflict {
		return decision(primary, "single", false, observations), nil
	}
	if conflict {
		s.log.Warn("IP "+ip+" 的属地来源存在未解决的分歧，不采用冲突的省市信息", "ip", ip, "evidence", observations)
		country := primary.Country
		for _, e := range observations {
			if e.Country != "" && e.Country != country {
				country = ""
				break
			}
		}
		return decision(Location{Country: country, Source: "unknown", QueriedAt: time.Now().UTC()}, "conflict", true, observations), nil
	}
	if partial.Country != "" {
		s.log.Warn("IP "+ip+" 的属地来源均未取得省级信息，保留 "+partial.Source+" 的部分结果", "ip", ip, "source", partial.Source)
		return decision(partial, "incomplete", false, observations), nil
	}
	return decision(Location{Source: "unknown", QueriedAt: time.Now().UTC()}, "unavailable", false, observations), errors.Join(ErrUnavailable, lastFailure)
}
