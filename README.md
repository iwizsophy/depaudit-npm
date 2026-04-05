# depaudit-npm

![depaudit-npm icon](./docs/assets/depaudit-npm-icon.png)

`depaudit-npm` is a cross-platform Go CLI that scans npm-related evidence sources to detect configured packages across repositories, lockfiles, installed packages, and optional npm cache logs.

You can scan for arbitrary packages through `targets.txt` or `--targets-file`.

Version `1.1.0` is the current stable release.

## Background

This tool was built in response to the 2026 npm supply chain attacks involving `axios` and `plain-crypto-js`.

At the time, sample code creation and package builds were happening during the affected window, so there was a real possibility that the local environment had been exposed. It was difficult to quickly determine whether that had actually happened because of a few practical gaps:

- There was no easy way to confirm not only direct dependencies but also transitive ones.
- A lockfile alone does not prove whether the package contents actually existed locally.
- Cross-checking `node_modules` and cache artifacts still required manual investigation.

Existing tools such as `Trivy`, `npm audit`, and `osv-scanner` are effective for vulnerability detection, but they are not designed for the narrower incident-response question of whether a specific package existed somewhere in the environment by any path.

`depaudit-npm` was created for that use case: incident-driven, evidence-based confirmation of package presence.

The goal of this tool is not to identify a specific vulnerability, but to confirm the fact of whether a package existed in the environment.

The default target definition file bundled with this tool already includes settings that can detect the `axios` / `plain-crypto-js` supply chain incident. You can use it as-is to immediately check whether your environment shows evidence related to that incident.

You can also modify the definition file to run the same style of investigation for any npm package.

The repository includes test data that corresponds to the `axios` / `plain-crypto-js` supply chain incident so the scan behavior can be validated. This does not affect normal use, but it is worth keeping in mind when reading the source tree.

The primary distribution model is a platform-specific release archive that
already contains the binary, `targets.txt`, the bundled documentation, and
`THIRD-PARTY-NOTICES.md`.
Each archive also includes a Syft-generated SBOM as `SBOM.spdx.json`.

## Features

- Cross-platform support for Windows, Linux, and macOS
- Structured parsing for `package-lock.json`, `npm-shrinkwrap.json`, `pnpm-lock.yaml`, `yarn.lock`, `bun.lock`, and `package.json`
- Optional `node_modules` package checks
- Optional npm cache log checks
- Findings, coverage, and project summaries written as separate output files
- Default bundled target list via `targets.txt`

## Quick Start

1. Download the `v1.1.0` release archive for your platform.
2. Extract the archive to a local folder.
3. Edit the bundled `targets.txt` if you want to change the default package list.
4. Run the binary from the extracted folder, or point it at another scan root.

Windows:

```powershell
.\depaudit-npm.exe
```

Linux:

```bash
./depaudit-npm
```

macOS:

```bash
./depaudit-npm
```

Examples:

Run in the current directory with the default target file:

```powershell
.\depaudit-npm.exe
```

Run against a specific path:

```powershell
.\depaudit-npm.exe --roots D:\src\myrepo
```

Run with an explicit target file:

```powershell
.\depaudit-npm.exe --targets-file .\custom-targets.txt --roots .
```

## Target File

The default target file name is `targets.txt`.
If `--targets-file` is omitted, the tool looks for `targets.txt` beside the executable first, and then in the current working directory.

In release archives, `targets.txt` is bundled next to the binary so the default configuration works immediately after extraction.

Each line contains one package spec:

```text
plain-crypto-js@4.2.1
axios@1.14.1
axios@0.30.4
@scope/pkg
```

Lines starting with `#` are ignored.

## Evidence Sources

Supported evidence sources:

- `package-lock.json`
- `npm-shrinkwrap.json`
- `pnpm-lock.yaml`
- `yarn.lock`
- `bun.lock`
- `package.json`
- `node_modules/<package>/package.json` when `--include-node-modules-folder-check` is enabled
- npm cache logs when `--include-npm-cache-logs` is enabled

`bun.lockb` is detected as unsupported evidence and reported as `NeedsReview`.

## Output Files

Each run writes:

- `npm_dependency_scan_<timestamp>.csv`
- `npm_dependency_scan_<timestamp>.findings.json`
- `npm_dependency_scan_<timestamp>.coverage.json`
- `npm_dependency_scan_<timestamp>.projects.json`

## How To Read The Results

Start with the console summary:

- `Overall assessment: Problem` means the scan found evidence that should be treated as a concrete hit.
- `Overall assessment: NeedsReview` means the scan found unsupported evidence, parsing/read failures, cache-log references, or other signals that require manual review.
- `Overall assessment: NoIssue` means no configured target package evidence was found in supported and successfully parsed sources.

Recommended reading order:

1. Check the terminal output for `Overall assessment` and the per-result summary counts.
2. Open `npm_dependency_scan_<timestamp>.findings.json` to inspect each finding in detail.
3. Open `npm_dependency_scan_<timestamp>.coverage.json` to confirm what was scanned, and whether any paths were skipped or unsupported.
4. Open `npm_dependency_scan_<timestamp>.projects.json` if you need a project-by-project view in a monorepo or multi-root scan.
5. Use `npm_dependency_scan_<timestamp>.csv` for spreadsheets, filtering, or handoff to non-JSON consumers.

How to interpret each file:

- `*.findings.json` is the primary output. Review `category`, `indicator`, `assessmentDetail`, `path`, and `result`.
- `*.coverage.json` explains scan coverage. If `skippedPathCounts` or `unsupportedEvidenceSourceCounts` is not empty, treat the run as incomplete and review those entries.
- `*.projects.json` groups findings by detected project root and shows the worst result for each project.
- `*.csv` contains the same findings as the JSON file in a flat table.

Important interpretation notes:

- `Problem` is the highest-severity result and takes precedence over all other results.
- `NeedsReview` does not always mean the target package was confirmed. It means the scan found something that should not be ignored without manual inspection.
- `NoIssue` only applies to supported evidence that was successfully parsed. It is not the same as "proven absent everywhere".
- `--strict` promotes otherwise clean `NoIssue` findings to `NeedsReview` when the run has coverage gaps or unsupported evidence sources.

## Build From Source

```powershell
go build -o .\bin\depaudit-npm.exe .
```

## Test

```powershell
go test ./...
```

## Documentation

- [Japanese README](./README.ja.md)
- [Setup](./docs/setup.md)
- [Development](./docs/development.md)
- [Third-party notices](./THIRD-PARTY-NOTICES.md)

If you only want to run the tool, use the release archive. Building from source is only needed for development or custom packaging.

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
Third-party license notices for bundled dependencies are listed in
[THIRD-PARTY-NOTICES.md](./THIRD-PARTY-NOTICES.md).
A Syft-generated SPDX JSON SBOM is distributed as
`SBOM.spdx.json` in each release archive.
