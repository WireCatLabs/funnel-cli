# Releasing

Seven packages go out together, at the version in `package.json`: `@wirecat/funnel` (the launcher) and
`@wirecat/funnel-<os>-<arch>` for `darwin`, `linux` and `win32` on `x64` and `arm64`. The launcher pins all six in
`optionalDependencies`. `node scripts/packages.mjs check` fails when a pin is not the version.

## Every release

1. A pull request bumps `version` and the six pins in `package.json` and adds a CHANGELOG entry. A change to
   `tailscale.com` also regenerates `THIRD_PARTY_NOTICES` (`node scripts/packages.mjs notices`) and runs
   `bin/live-check` (see [TESTING.md](TESTING.md)).
2. After it merges, start the release: `gh workflow run release.yml -f version=<version>`.
3. `release.yml` runs CI with packing, checks each tarball's SHA-256, publishes the six platform packages and then the
   launcher with `--provenance`, and tags `v<version>`. npm signs in through trusted publishing (GitHub's OIDC token,
   no npm token in GitHub).
4. Check: `npm view @wirecat/funnel version` and `npm audit signatures` in a project that installs it. WireCat Desktop
   refuses a version that has no provenance from this repository's `release.yml`.

## The first release

npm sets a trusted publisher only on a package that exists (WC-REL-11 in cli-base's release standards), so the first
version is published by the owner from his computer:

```sh
/path/to/funnel-cli/bin/release-first
```

It builds and publishes the seven packages at the current version from a clean, up-to-date `main` (npm asks for the
one-time code), then adds `release.yml` as the trusted publisher of each. That version has no provenance, so Desktop
does not install it: release the next version through `release.yml` right after.

Provenance needs a public repository.
