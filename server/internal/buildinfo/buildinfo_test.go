package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestFromReadsRevisionAndModified(t *testing.T) {
	got := from([]debug.BuildSetting{{Key: "vcs.revision", Value: "abc123"}, {Key: "vcs.modified", Value: "true"}, {Key: "GOOS", Value: "darwin"}})
	if got.VCSRevision != "abc123" || !got.VCSModified {
		t.Fatalf("%+v", got)
	}
	if got := from(nil); got.VCSRevision != "" || got.VCSModified {
		t.Fatalf("empty settings: %+v", got)
	}
}

func TestExplicitLinkTimeIdentityWinsAndModifiedFailsClosed(t *testing.T) {
	defer func(r, m string) { revision, modified = r, m }(revision, modified)
	revision, modified = "deadbeef", "false"
	if got := Read(); got.VCSRevision != "deadbeef" || got.VCSModified {
		t.Fatalf("%+v", got)
	}
	// Anything but an explicit "false" counts as modified: a missing flag never
	// makes a build look clean.
	revision, modified = "deadbeef", ""
	if got := Read(); !got.VCSModified {
		t.Fatalf("%+v", got)
	}
	revision, modified = "deadbeef", "true"
	if got := Read(); !got.VCSModified {
		t.Fatalf("%+v", got)
	}
}

// Run with the real link flags to prove the -X path names are right:
//
//	go test -ldflags "-X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.revision=cafebabe -X github.com/bianjiefilm/product-image-engine/server/internal/buildinfo.modified=false" -run LinkTimeFlags ./internal/buildinfo
func TestLinkTimeFlagsAreWiredWhenGiven(t *testing.T) {
	if revision == "" {
		t.Skip("no -ldflags given")
	}
	if got := Read(); got.VCSRevision != revision || got.VCSModified != (modified != "false") {
		t.Fatalf("%+v", got)
	}
}
