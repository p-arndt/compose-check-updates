# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## 0.13.0 - 2026-09-05

### Added

- A digest-pinned tag that was re-pushed is reported as an update, even though its version never changed
- Every update now carries the link to its release notes: `release_url` and `source_url` in the JSON output, read from the image's `org.opencontainers.image.source` label, and shown in the TUI detail column
- Every update shows when its tag was published (`published` in the JSON output, `3d ago` in the report and the TUI), and `min_age` — run-wide, per image or `-min-age 7d` — holds back tags younger than that in favour of the newest one old enough
- `-image` restricts a run to matching images: `ccu check -image traefik` finds `library/traefik`, `-image 'ghcr.io/immich-app/*'` globs the full name, and filtered-out images cost no registry request
- Image tags kept in a variable now work: `app:${APP_VERSION}` is resolved from the `.env` beside the compose file, and an update is written back to that `.env` line
- Registry answers are cached briefly on disk, so reopening the TUI or running a check right after one no longer costs a round of rate-limited lookups; -refresh bypasses it

### Changed

- A tag built from a variable nothing defines now names the missing variable and the `.env` it belongs in

### Fixed

- An image that is the only release in its repository is no longer reported as unreadable
- Capping an image in the sidebar no longer makes its recorded versioning read as default until the next start
- ccu version prints to stdout, so the version can be captured in a script
- Log lines with several extra details keep the order they were logged in instead of shuffling between runs
- The bar's apply count and the N selected status line no longer count rows the apply will skip, such as one that became unreadable on a re-check or lost its target.
- The warning about serving cached registry data now shows how old that data is
