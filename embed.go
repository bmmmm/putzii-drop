// SPDX-License-Identifier: GPL-3.0-or-later

// Package putziidrop exposes the repo skeleton that `dropii setup` pushes
// into a fresh drop repo. Source of truth are the REAL files in this repo —
// embedded at build time, so a dropii binary is self-contained.
package putziidrop

import "embed"

// Skeleton holds everything a fresh drop repo needs. site/ is embedded
// selectively: health.json and plans/ carry live state and must NOT seed a
// new drop.
//
//go:embed .github/workflows all:runner site/index.html site/robots.txt site/.nojekyll LICENSE .gitignore
var Skeleton embed.FS

// SkeletonFiles lists the paths (relative repo paths) setup writes.
func SkeletonFiles() ([]string, error) {
	var out []string
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := Skeleton.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			p := e.Name()
			if dir != "." {
				p = dir + "/" + p
			}
			if e.IsDir() {
				if err := walk(p); err != nil {
					return err
				}
				continue
			}
			out = append(out, p)
		}
		return nil
	}
	if err := walk("."); err != nil {
		return nil, err
	}
	return out, nil
}
