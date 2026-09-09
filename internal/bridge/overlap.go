package bridge

import (
	"fmt"
	"path/filepath"
	"strings"
)

type PathClaim struct {
	Profile string `json:"profile"`
	Role    string `json:"role"`
	Path    string `json:"path"`
}

type PathOverlap struct {
	First  PathClaim `json:"first"`
	Second PathClaim `json:"second"`
}

type OverlapReport struct {
	Version  int           `json:"version"`
	ReadOnly bool          `json:"readOnly"`
	Profiles []string      `json:"profiles"`
	Overlaps []PathOverlap `json:"overlaps"`
}

// CheckOverlaps compares only the explicitly supplied profiles. It neither
// scans homes nor registers ownership. Case folding is deliberately conservative.
func CheckOverlaps(files []string) (OverlapReport, error) {
	r := OverlapReport{Version: 1, ReadOnly: true, Profiles: []string{}, Overlaps: []PathOverlap{}}
	if len(files) < 2 {
		return r, fmt.Errorf("at least two profiles required")
	}
	groups := [][]PathClaim{}
	seen := map[string]bool{}
	for _, file := range files {
		absolute, err := filepath.Abs(file)
		if err != nil {
			return r, err
		}
		key := strings.ToLower(absolute)
		if seen[key] {
			return r, fmt.Errorf("duplicate profile")
		}
		seen[key] = true
		c, err := LoadAuditConfig(absolute)
		if err != nil {
			return r, err
		}
		claims := []PathClaim{{absolute, "state", c.StateDir}}
		for _, config := range c.ConfigFiles {
			claims = append(claims, PathClaim{absolute, "config", config})
		}
		if c.CoordinationDir != "" {
			claims = append(claims, PathClaim{absolute, "coordination", c.CoordinationDir})
		}
		for _, resource := range c.Resources {
			for _, side := range sides[1:] {
				claims = append(claims, PathClaim{absolute, resource.ID + ":" + side, resource.Paths[side]})
				if link, ok := resource.Links[side]; ok {
					claims = append(claims, PathClaim{absolute, resource.ID + ":" + side + ":link", link.Path})
				}
			}
		}
		for _, claim := range claims {
			if err := assertSafeClaim(claim); err != nil {
				return r, err
			}
		}
		groups = append(groups, claims)
		r.Profiles = append(r.Profiles, absolute)
	}
	for i, group := range groups {
		for _, other := range groups[i+1:] {
			for _, a := range group {
				for _, b := range other {
					// Shared inherited configuration and shared coordination are intentional.
					if a.Path == b.Path && a.Role == b.Role && (a.Role == "config" || a.Role == "coordination") {
						continue
					}
					x, y := strings.ToLower(a.Path), strings.ToLower(b.Path)
					if inside(x, y) || inside(y, x) {
						r.Overlaps = append(r.Overlaps, PathOverlap{a, b})
					}
				}
			}
		}
	}
	return r, nil
}

func assertSafeClaim(c PathClaim) error {
	// Pinned link targets were validated by the loader; the link itself is
	// intentionally a symlink, but its ancestors must still be safe.
	if strings.HasSuffix(c.Role, ":link") {
		return assertSafe(filepath.Dir(c.Path))
	}
	return assertSafe(c.Path)
}
