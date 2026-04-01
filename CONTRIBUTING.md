# Contributing

## Scope

This repository provides a cross-platform CLI tool that scans npm-related evidence sources to find configured packages and produce findings, coverage, and project summaries.

Keep changes aligned with that scope:

- npm/package-manager evidence scanning
- cross-platform CLI behavior
- deterministic output files
- packaging and release automation for the CLI

## Before You Start

- For general usage questions and non-security support, see `.github/SUPPORT.md`
- For vulnerability reports, follow `SECURITY.md`
- For collaboration expectations, follow `CODE_OF_CONDUCT.md`
- Japanese contributor guidance is available in `README.ja.md`

## Local Development

### Build

```powershell
go build -o .\bin\depaudit-npm.exe .
```

### Test

```powershell
go test ./...
```

### Cross-platform build check

```powershell
$env:GOOS='windows'; $env:GOARCH='amd64'; go build .
$env:GOOS='linux'; $env:GOARCH='amd64'; go build .
$env:GOOS='darwin'; $env:GOARCH='arm64'; go build .
Remove-Item Env:GOOS
Remove-Item Env:GOARCH
```

## Change Guidelines

- Keep default scan behavior explicit and documented.
- Do not silently widen or narrow supported evidence sources without updating tests and docs together.
- Preserve output file schema stability unless a documented versioned change is necessary.
- Prefer standard-library-first implementations unless a third-party dependency is clearly justified.

## Docs Expectations

When behavior changes, update the relevant docs in the same change:

- `README.md` for public overview and quick start
- `README.ja.md` for Japanese usage notes
- `docs/setup.md` for setup and packaging expectations
- `docs/development.md` for contributor-facing notes
- `CHANGELOG.md` for user-visible changes

## Testing Expectations

Before opening a PR, run:

```powershell
go test ./...
```

If you changed packaging, release, or cross-platform behavior, also validate at least one relevant `go build` target locally.

## Pull Requests

A good pull request should include:

- a short summary of the user-visible change
- why the change belongs in this repository
- any output schema or CLI contract impact
- platform-specific impact, if any
- doc updates when behavior changed
- test or verification notes
