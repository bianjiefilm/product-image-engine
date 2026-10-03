// Package buildinfo reports the code identity compiled into the binary so that
// acceptance evidence can prove which commit produced a result.
//
// Go's own VCS stamping is not trusted here: inside a linked git worktree it can
// walk up to the main repository and stamp the wrong commit. Release and
// acceptance builds therefore pass the identity explicitly:
//
//	go build -buildvcs=false -ldflags "\
//	  -X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.revision=$(git rev-parse HEAD) \
//	  -X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.modified=$([ -n "$(git status --porcelain)" ] && echo true || echo false)" ./cmd/server
package buildinfo

import "runtime/debug"

// Set only by -ldflags -X (string variables are the only kind -X can set).
var revision, modified string

type Info struct {
	VCSRevision string
	VCSModified bool
}

// Read returns the explicit link-time identity when present, else whatever the
// toolchain stamped. A binary built without either reports an empty revision,
// which acceptance preflight refuses.
func Read() Info {
	if revision != "" {
		return Info{VCSRevision: revision, VCSModified: modified != "false"}
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return Info{}
	}
	return from(bi.Settings)
}

func from(settings []debug.BuildSetting) Info {
	var out Info
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			out.VCSRevision = s.Value
		case "vcs.modified":
			out.VCSModified = s.Value == "true"
		}
	}
	return out
}
