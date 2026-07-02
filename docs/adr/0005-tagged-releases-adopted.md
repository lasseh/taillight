# Tagged releases adopted; the no-releases covenant is reversed

Until 2026-07 the CHANGELOG carried an explicit covenant: continuous deploy, no version tags, `[Unreleased]` as a permanent running log. In practice the covenant was already breached — tag `py-v0.1.0` + `python-publish.yml` publish `taillight-sdk` to PyPI on `py-v*` tags, and a `make release` target + `release.yml` had been added (commit `cc6616c`) without ever firing.

Maintainer decision (architecture review 2026-07-02, D6): *"lets go for releases, remove the 'no release tags' and implement what we need for this to work."* Landed in commit `b2af891`:

- CHANGELOG convention amended — `[Unreleased]` accumulates; cutting `vX.Y.Z` moves entries under a dated version heading.
- `make release` slimmed to test + tag + push; `.github/workflows/release.yml` builds the cross-platform `taillight` + `taillight-shipper` binaries, the GitHub release, and the Docker image on `v*` tag push.
- Semver `vX.Y.Z` for the server + shipper; `sdk/python` keeps its independent `py-v*` PyPI channel; `python-publish.yml` untouched.

Continuous deploy to production continues unchanged — tags are for self-hosters and shipper-binary distribution, not a gate on deployment.

See `.scratch/architecture-review/REPORT.md` D6.
