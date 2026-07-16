# Download Source Policy

XDev manifests must identify concrete, reproducible payloads. A reachable host or mirror root is not evidence that an archive exists.

## Source Classes

- **Official source:** A fixed HTTPS URL controlled by the upstream project or vendor.
- **Official release asset:** A versioned asset attached to the upstream project's release page, including an official GitHub Release.
- **Trusted mirror:** A maintained institutional or vendor mirror serving the same concrete bytes as the official source.
- **Archive site:** An upstream-controlled historical download location used when the primary release channel no longer carries a fixed version.
- **Unacceptable third party:** File-sharing sites, repackagers, mutable personal storage, URL shorteners, or any source whose ownership and payload provenance cannot be established.

Source precedence is official stable source, official release asset, trusted byte-identical mirror, then official archive site. If none is reliable, mark the version unavailable rather than adding an untrusted source.

## Transport And Redirects

Production URLs must use HTTPS. Redirects may remain on HTTPS and are limited to ten hops. HTTPS-to-HTTP downgrade is always rejected. Tests may use local HTTP servers only through the auditor's explicit test injection boundary.

The auditor sends HEAD first. When HEAD is unsupported, forbidden, or does not provide conclusive payload evidence, it sends `Range: bytes=0-65535`. Health is assessed against the concrete archive URL, not a service root.

## Evidence Levels

Probe mode records status, final URL, content type, content length, range behavior, and payload magic bytes. ZIP, gzip, xz, 7z, executable, and script signatures are recognized. HTML returned in place of an archive is a deterministic failure. Content type is advisory: valid magic bytes take precedence over an incorrect generic content type.

Probe mode never claims SHA-256 verification. Full mode streams the complete response into a controlled cache while calculating SHA-256; archives are not buffered entirely in memory. A manifest hash matches only after a complete download. Identical URLs share one network transfer while retaining one result row for every manifest declaration.

Manifest mirror values must name a concrete artifact. Mirror roots and templates are reported as `IMPLEMENTATION_MISSING` until an explicit rewrite rule produces a concrete URL; the auditor never probes a root as if it were an archive.

## SHA-256 Requirements

Every released payload must have a complete SHA-256 value obtained from an official checksum or a complete audited download. Never infer or update a hash from a range probe. Identical normalized URLs must declare identical hashes across all manifests and platforms.

Full mode reports `MISSING_CHECKSUM` for an empty or malformed expected SHA-256 and never reports such a row as passing. This is a deterministic release failure.

## Release Gates

- Every Windows amd64 built-in payload must pass a full audit before release.
- Other declared platforms must pass a probe and must not claim unperformed SHA verification.
- `404`, `410`, invalid archive bytes, HTML payloads, unsafe redirects, and hash mismatches are deterministic release failures.
- Timeouts, DNS failures, redirect loops, and server `5xx` responses are transient failures and require retry or explicit release review.
- Changed manifest URLs, versions, platform mappings, or hashes require regeneration of the corresponding manifest signature.
- JSON and Markdown reports must be retained as build artifacts; reports are evidence, not source-of-truth manifest data.
