# Setup

## Prerequisites

- The `v1.0.0` release archive for your platform
- File-system access to the repositories or directories you want to scan

You do not need Go to use the packaged binary. Go is only required when building from source.

The first public stable release is `v1.0.0`.

## Release Archive Contents

Each release archive is expected to contain:

- the platform binary
- `targets.txt`
- `README.md`
- `README.ja.md`
- `LICENSE`
- `THIRD-PARTY-NOTICES.md`
- `SBOM.spdx.json`
- `docs/assets/depaudit-npm-icon.png`

Extract the archive to a local directory before running the tool.

## Default Configuration

The tool uses `targets.txt` by default.

Resolution order:

1. `targets.txt` beside the executable
2. `targets.txt` in the current working directory

To override this, use `--targets-file`.

## Typical Commands

Run the extracted binary from the folder where the archive was unpacked.

Scan the current directory:

```powershell
.\depaudit-npm.exe
```

Scan multiple roots:

```powershell
.\depaudit-npm.exe --roots repoA repoB repoC
```

Scan with optional installed-package verification:

```powershell
.\depaudit-npm.exe --roots . --include-node-modules-folder-check
```

Scan with npm cache logs:

```powershell
.\depaudit-npm.exe --include-npm-cache-logs
```

This layout ensures the bundled default target file works when users launch the binary directly from the extracted folder.
