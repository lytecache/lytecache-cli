module github.com/lytecache/lytecache-cli

go 1.25.0

require (
	github.com/chzyer/readline v1.5.1
	// v0.3.0, not the already-tagged v0.2.0: the CLI needs Cache.Inspect and
	// Cache.Maintain, which only exist in lytecache-go's [Unreleased] section
	// as of this writing. lytecache-go must cut v0.3.0 before this version
	// resolves for real -- see the ordering note in RELEASING.md.
	github.com/lytecache/lytecache-go v0.3.0
	github.com/spf13/cobra v1.10.2
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/sys v0.44.0 // indirect
	modernc.org/libc v1.73.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
	modernc.org/sqlite v1.53.0 // indirect
)

// Local development only: lytecache-go lives in the sibling directory of this
// monorepo. Remove this line once a real v0.2.0+ tag exists on the
// standalone github.com/lytecache/lytecache-go repo (see RELEASING.md) --
// until then, the `require` version above is aspirational, not fetchable
// from the proxy, and this replace is what makes `go build`/`go test` work
// against the real, current source instead.
replace github.com/lytecache/lytecache-go => ../lytecache-go
