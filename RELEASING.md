# Releasing

There is no release tooling: a release is a git tag plus a GitHub release.
Versions are full semver with a `v` prefix: `v1.5.0`, not `v1.5`.

## Steps

```
go build ./... && go test ./...
git tag v0.1.0
git push origin v0.1.0
gh release create v0.1.0 --title "V0.1" --notes "First release: MC68000 core, disassembler, Harte test suite"
```
## Consumers

Dependent projects pin the version in their `go.mod`:

```bash
go get github.com/ivanizag/iz68000@v0.1.0
```
