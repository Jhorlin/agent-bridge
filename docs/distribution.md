# Building and verifying release archives

No tagged release or public binary assets have been published yet. The repository
now includes a Go-only packager; it builds archives but never installs a service,
changes Claude/Codex settings, creates a tag, or publishes to GitHub.

## Maintainer packaging

From a clean, reviewed repository checkout with Go 1.25 or newer:

```sh
go test -race ./...
go vet ./...
go run ./cmd/package-release -version v0.1.0-alpha.1 -out dist
```

The version above is a proposed first prerelease identifier, not an existing
release. The output directory must not exist; use a different new path for a
second build. On failure, partial archives remain for inspection. Checksums are
published locally only after all four archives are successfully created. Packaging
does not delete existing build outputs or copy local profiles/state/backups.

Outputs are `agent-bridge_VERSION_OS_ARCH.tar.gz` for `darwin`/`linux` and
`amd64`/`arm64`, plus `SHA256SUMS`. Each archive contains only:

- `agent-bridge` (executable mode 0755)
- `LICENSE`
- `README.md`

Archives have normalized names, timestamps and modes. Builds use `CGO_ENABLED=0`,
`-mod=readonly`, `-trimpath`, `-buildvcs=false`, and an embedded version. Reproducing
binary hashes requires the same source and Go toolchain; normalized archive
metadata alone does not establish supply-chain provenance. There are no signatures,
macOS notarization, SBOMs or attestations in this initial packaging path.

The tests verify checksums, archive entries/modes, deterministic archive construction,
failure preservation and no-overwrite behavior. This opt-in test also cross-builds
all four targets and runs the matching local binary's version command:

```sh
AGENT_BRIDGE_PACKAGE_TESTS=1 go test ./internal/release -run '^TestNativePackage$' -v
```

CI performs the real packaging smoke test on macOS and Linux. This runs native
architecture binaries only; other architecture builds are inspected as archives,
not claimed to have run on that machine.

## User verification and installation

Once reviewed assets are published, obtain the archive matching your OS/CPU and
`SHA256SUMS` from that same release. `darwin` means macOS; Apple Silicon uses
`arm64`, Intel Macs use `amd64`. Linux machines use the matching CPU architecture.

Example for the proposed Apple Silicon prerelease, from a download directory:

```sh
shasum -a 256 agent-bridge_v0.1.0-alpha.1_darwin_arm64.tar.gz
```

Compare the entire output digest with that archive's line in `SHA256SUMS` before
extracting. Linux can use `sha256sum` instead. If you downloaded all four archives,
`shasum -a 256 -c SHA256SUMS` verifies the full set. Checksums detect corruption;
they do not authenticate a compromised release or replace a signature.

Extract into a new empty directory you control, inspect the contents, then run:

```sh
./agent-bridge --version
```

The standalone binary does not require Go. It needs no runtime package-manager
installation. Move it to a stable location before installing a background service.
On macOS, unsigned downloaded binaries may require review through system security
controls; this guide does not recommend bypassing them blindly.

Use the online [onboarding guide](https://github.com/Jhorlin/agent-bridge/blob/main/docs/onboarding.md)
and [service guide](https://github.com/Jhorlin/agent-bridge/blob/main/docs/services.md).
README-relative documentation and demo paths refer to the source checkout, not
files bundled in the binary archive. Clone the repository to run source examples.

Ordinary source builds report `agent-bridge dev`; the packager embeds the requested
version. A version label is not a signature or proof of an official release.

## Publication gate

Before public publication: choose/confirm a version and prerelease status, ensure
the source commit and CI are verified, repeat required native fixture checks,
review the exact artifacts/checksums, and publish only those assets with accurate
experimental limitations. The packager does not automate or imply this approval.
