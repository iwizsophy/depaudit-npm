# Third-Party Notices

This document lists third-party Go modules currently included in the
repository dependency graph through `go.mod`.

## Scope

- Listed items cover modules explicitly present in `go.mod`.
- The Go standard library is not listed here.
- Additional transitive dependencies are reviewed during dependency updates and
  release validation, but are not listed separately by default.

## Current modules

### gopkg.in/yaml.v3 v3.0.1

- License: Apache License 2.0
- Additional notice: parts of the module include code ported from libyaml under
  the MIT License
- Source: `gopkg.in/yaml.v3`

## Update policy

- Update this file when a dependency is added, removed, or its version changes
  in `go.mod`.
- Re-check license terms when dependency versions change.
- If a module ships multiple notices or mixed-license files, summarize that
  fact here and retain the upstream notice requirements in distributed
  materials when applicable.
