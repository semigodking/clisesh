# Spec 013 — CI/CD & Automated Release Packaging

Status: Implemented

## Purpose

Automate continuous integration testing, lint checks, multi-architecture cross-compilation, and GitHub Release asset packaging for `clisesh`.

## CI Pipeline (`.github/workflows/ci.yml`)

Triggered on pushes and pull requests targeting the `main` branch.
- Checks out code with full history.
- Sets up Go using `go.mod` specification.
- Runs full test suite: `go test -v ./...`.
- Verifies code style: `go vet ./...` and `gofmt -l .`.
- Verifies build: `make build && ./bin/clisesh version`.

## Release Pipeline (`.github/workflows/release.yml`)

Triggered when a Git tag matching `v[0-9]+.[0-9]+.[0-9]+*` is pushed.

### Cross-Compilation Matrix

| Target OS | Target Architecture | Archive Package Name |
| :--- | :--- | :--- |
| `linux` | `amd64` (x86_64) | `clisesh_${VERSION}_linux_amd64.tar.gz` |
| `linux` | `arm64` (AArch64) | `clisesh_${VERSION}_linux_arm64.tar.gz` |
| `darwin` | `amd64` (Intel macOS) | `clisesh_${VERSION}_darwin_amd64.tar.gz` |
| `darwin` | `arm64` (Apple Silicon) | `clisesh_${VERSION}_darwin_arm64.tar.gz` |

### Packaging Rules
- Binary compiled with `CGO_ENABLED=0`, `-trimpath`, and `-ldflags "-s -w -X main.version=${VERSION}"`.
- Each archive includes the binary executable, `README.md`, and `README.zh-CN.md`.
- Generates `checksums.txt` containing SHA256 checksums for all archive files.
- Automatically creates a GitHub Release via `softprops/action-gh-release@v2` with generated release notes.

## Local Dist Target

Developers can run `make dist` locally to reproduce the release cross-compilation and packaging steps into the `dist/` folder.
