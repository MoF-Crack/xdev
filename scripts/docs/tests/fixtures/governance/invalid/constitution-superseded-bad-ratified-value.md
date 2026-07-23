# XDev Constitution (Test Fixture - Superseded with Bad Ratified Value)
# This fixture MUST FAIL: Superseded state with a non-date Ratified field value (e.g. "some-string")

## Document Status

| Field | Value |
|---|---|
| Project | XDev |
| Version | `1.0.0` |
| State | Superseded |
| Candidate Date | 2026-07-15 |
| Ratified Date | some-non-date-string |
| Superseded By | 2.0.0 |
| Approver | `xinyan <zuijiuhjd@foxmail.com>` |

This fixture tests that a Superseded state constitution with Ratified="some-non-date-string" is rejected.
Superseded state requires Ratified to be a valid YYYY-MM-DD date (the original ratification date).

## Non-Negotiable Principles

### I. Security and Trust Boundary

Test fixture principle content. No placeholders.

### II. Tests Must Isolate Host State

Test fixture principle content.

## Approval History

| Phase | Status | Approved By | Date | Doc Version | Approval Scope |
|---|---|---|---|---|---|
| Constitution Structure | Candidate Draft Authorized | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-15 | `1.0.0-rc.1` | Test fixture |
| Constitution Ratification | Ratified | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `1.0.0` | Formal ratification |

**Version**: `1.0.0` | **Candidate Date**: 2026-07-15 | **Ratified**: some-non-date-string | **Superseded By**: 2.0.0 | **Last Amended**: 2026-07-18
