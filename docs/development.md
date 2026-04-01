# Development

## Project Layout

- `main.go`: CLI option parsing and top-level orchestration
- `scan.go`: candidate file enumeration and project-context detection
- `analyze.go`: evidence parsing and finding generation
- `report.go`: output generation and summary formatting
- `testdata/fixtures`: regression fixtures used by tests

## Development Workflow

Run formatting and tests before pushing changes:

```powershell
gofmt -w *.go
go test ./...
```

## Design Notes

- Favor deterministic output over aggressive heuristics.
- Prefer structured parsing for supported lockfiles.
- Treat unreadable or malformed evidence as review-worthy instead of silently ignoring it.
- Keep `node_modules` package manifests out of project-root inference.

## CI Expectations

The CI workflow validates:

- formatting
- tests
- matrix builds on Windows, Linux, and macOS

The release workflow builds tagged archives and publishes them to GitHub Releases.
