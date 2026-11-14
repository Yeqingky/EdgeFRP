# Release Process

## 1. Update Release Notes

Edit `Release.md` in the project root with the changes for this version:

```markdown
## Features
* ...

## Improvements
* ...

## Fixes
* ...
```

This file is used by GoReleaser as the GitHub Release body.

## 2. Bump Version

Update the version string in `pkg/util/version/version.go`:

```go
var version = "0.X.0"
```

Commit and push to `master`:

```bash
git add pkg/util/version/version.go Release.md
git commit -m "bump version to vX.Y.Z"
git push origin master
```

## 3. Pre-release Validation

Run the standard e2e suite locally:

```bash
make e2e
```

## 4. Tag the Release

```bash
git checkout master
git pull origin master
git tag -a vX.Y.Z -m "bump version"
git push origin vX.Y.Z
```

Pushing the `v*` tag triggers the `goreleaser` workflow automatically.

## 5. GoReleaser

GoReleaser will:
1. Run `package.sh` to cross-compile all platforms and create archives
2. Create a GitHub Release with all packages, using `Release.md` as release notes

## Key Files

| File | Purpose |
|------|---------|
| `pkg/util/version/version.go` | Version string |
| `Release.md` | Release notes (read by GoReleaser) |
| `.goreleaser.yml` | GoReleaser config |
| `package.sh` | Cross-compile and packaging script |
| `.github/workflows/goreleaser.yml` | GitHub Actions workflow (manual trigger) |

## Versioning

- Minor release: `v0.X.0`
- Patch release: `v0.X.Y` (e.g., `v0.62.1`)
