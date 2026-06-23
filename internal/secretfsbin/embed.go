// Package secretfsbin embeds the prebuilt devc-secretfs FUSE helper so devc can
// copy it into a container at runtime — the image needs no extra packages.
//
// The Linux binaries are built by `make secretfs-bin` (and by the release
// pipeline) into embedded/. They are gitignored; the committed
// embedded/.gitkeep keeps the `all:embedded` embed valid when they are absent,
// in which case Binary returns an error and devc reports that secret hiding is
// unavailable instead of failing to build. (The dir is not named bin/ or dist/
// because the repo's .gitignore excludes those.)
package secretfsbin

import (
	"embed"
	"fmt"
)

//go:embed all:embedded
var binFS embed.FS

// Binary returns the embedded devc-secretfs helper for the given GOARCH
// ("amd64" or "arm64"), or an error if it was not built into this devc binary.
func Binary(goarch string) ([]byte, error) {
	name := "embedded/devc-secretfs-linux-" + goarch
	data, err := binFS.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("embedded secretfs helper for linux/%s not available (build with `make secretfs-bin`): %w", goarch, err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("embedded secretfs helper for linux/%s is empty", goarch)
	}
	return data, nil
}
