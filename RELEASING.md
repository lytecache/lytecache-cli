# Releasing

`lytecache-cli` is published as a standalone repo at [github.com/lytecache/lytecache-cli](https://github.com/lytecache/lytecache-cli), separate both from this monorepo and from [lytecache-go](https://github.com/lytecache/lytecache-go) (which it depends on like any other consumer, not as a sibling module) -- because `go install`/`pkg.go.dev`/Homebrew/Scoop/winget all need one plain `vX.Y.Z` tag history to point at.

## Ordering dependency on lytecache-go

`go.mod` here has a `replace github.com/lytecache/lytecache-go => ../lytecache-go` line so this module builds against the live sibling directory during local/monorepo development. That line only makes sense inside this monorepo -- it must not (and, per the release workflow below, does not) reach the standalone repo, since consumers there have no `../lytecache-go` to resolve against.

Before releasing `lytecache-cli`, the version named in `go.mod`'s `require github.com/lytecache/lytecache-go` line must already be tagged and published on the standalone `lytecache-go` repo (see [lytecache-go/RELEASING.md](../lytecache-go/RELEASING.md)) -- the release workflow drops the `replace` and re-resolves against the real module, which fails if that version doesn't exist yet.

## One-time setup

1. Create `lytecache/lytecache-cli` on GitHub (empty is fine -- the release workflow pushes to it).
2. Create `lytecache/homebrew-tap` and `lytecache/scoop-bucket` repos (each can start empty; GoReleaser commits the formula/manifest on first release).
3. Generate three fine-grained GitHub PATs, each scoped to exactly one repo (matching this project's one-token-per-target-repo convention -- see `SPLIT_REPO_TOKEN` for lytecache-php), with Contents: Read and write (and Metadata: Read):
   - `CLI_SPLIT_REPO_TOKEN` -- scoped to `lytecache/lytecache-cli` (push the split code/tags, create the GitHub release and its assets)
   - `HOMEBREW_TAP_TOKEN` -- scoped to `lytecache/homebrew-tap`
   - `SCOOP_BUCKET_TOKEN` -- scoped to `lytecache/scoop-bucket`
4. Add all three as repository secrets on the monorepo (Settings -> Secrets and variables -> Actions). `.github/workflows/lytecache-cli-release.yml` reads them.

That's the entire one-time setup -- everything else happens automatically from a tag push.

## Cutting a release

1. Make sure `lytecache-go` has already released the version named in `go.mod`'s `require` line (see above) -- otherwise skip to step 2 of [lytecache-go/RELEASING.md](../lytecache-go/RELEASING.md) first.
2. Make sure `main` is green: `go build ./...`, `go vet ./...`, and `go test -race ./...` (from `lytecache-cli/`) all pass -- CI enforces this on every push, but verify locally before tagging.
3. Update [CHANGELOG.md](CHANGELOG.md): move the `[Unreleased]` section's contents under a new `## [x.y.z] - YYYY-MM-DD` heading.
4. Commit the changelog update.
5. Tag using the monorepo-subdirectory convention (`lytecache-cli/vX.Y.Z`), distinct from every other component's tag prefix so tagging one release can never trigger another's workflow:
   ```bash
   git tag lytecache-cli/v0.1.0
   git push origin main --tags
   ```
6. `.github/workflows/lytecache-cli-release.yml` then:
   - splits `lytecache-cli/` out via `git subtree split`.
   - drops the local `replace` directive in the split copy's `go.mod`, runs `go mod tidy` against the now-real `lytecache-go` dependency, and commits that fix -- so the copy that reaches the standalone repo builds for any consumer, not just inside this monorepo.
   - pushes that branch, along with a **plain** `v0.1.0` tag (the `lytecache-cli/` prefix is stripped -- it only exists to disambiguate tags within this monorepo), to `lytecache/lytecache-cli`.
   - checks out the standalone repo at that tag and runs `go test -race ./...` there as a final sanity check (this is what actually proves the dropped-replace dependency resolves).
   - runs GoReleaser, which builds binaries for linux/darwin (amd64/arm64), packages them as `.tar.gz`/`.zip` with SHA-256 checksums, builds `.deb`/`.rpm` packages, generates a winget manifest, and publishes all of it as GitHub release assets on `lytecache/lytecache-cli` -- plus opens/updates the Homebrew cask in `lytecache/homebrew-tap` and the Scoop manifest in `lytecache/scoop-bucket`.
7. Verify: `go install github.com/lytecache/lytecache-cli/cmd/lytecache@latest` and `brew install lytecache/tap/lytecache` both resolve to the new version.

## Submitting to `microsoft/winget-pkgs`

The release workflow generates a winget manifest and attaches it to the GitHub release as a plain asset (`skip_upload: true` in `.goreleaser.yaml`) rather than opening a PR against `microsoft/winget-pkgs` automatically -- that repo requires human review for every new package and every version, which isn't something CI should do unattended for a package this new.

Once the CLI has a track record (a handful of stable releases, real users), submit it manually:

1. Download the winget manifest files from the latest release's assets (`*.yaml` under the manifest path).
2. Follow [microsoft/winget-pkgs' contributing guide](https://github.com/microsoft/winget-pkgs/blob/master/CONTRIBUTING.md) to open a PR adding them (or use `wingetcreate` to regenerate them against the release's actual asset URLs/hashes).
3. After the first manifest is accepted, GoReleaser's generated manifest can be submitted the same way on each subsequent release, or winget's own auto-update bots may pick up new versions automatically once the package is established.

## Versioning notes

- **Public API** covers everything documented in [README.md](README.md): command names, flags, output formats, and exit codes. Scripts depend on these -- a change to any of them is a breaking change and requires a major version bump once past `1.0.0`.
- Versioned independently from `lytecache-go` -- a `lytecache-go` release doesn't require a matching `lytecache-cli` release, and vice versa. `go.mod`'s `require` line simply names the minimum library version this CLI needs (currently for `Cache.Inspect`/`Cache.Maintain`).
- Before `1.0.0`, minor versions (`0.x.0`) may include breaking changes, per semver's `0.y.z` convention.
