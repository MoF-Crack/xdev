# XDev Constitution (Test Fixture - Candidate with Date)
# This fixture MUST FAIL: Candidate state with a non-Pending Ratified field (date instead of Pending)

## Document Status

| Field | Value |
|---|---|
| Project | XDev |
| Version | `1.0.0-rc.4` |
| State | Candidate |
| Candidate Date | 2026-07-18 |
| Ratified Date | 2026-07-18 |
| Approver | `xinyan <zuijiuhjd@foxmail.com>` |

This fixture tests that a Candidate state constitution with Ratified=2026-07-18 (a date) is rejected.
Candidate state requires Ratified field to be "Pending", not a date value.

## Non-Negotiable Principles

### I. Security and Trust Boundary

Test fixture principle content. No placeholders.

### II. Tests Must Isolate Host State

Test fixture principle content.

## Approval History

| Phase | Status | Approved By | Date | Doc Version | Approval Scope |
|---|---|---|---|---|---|
| Constitution Structure | Candidate Draft Authorized | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-18 | `1.0.0-rc.4` | Test fixture |

**Version**: `1.0.0-rc.4` | **Candidate Date**: 2026-07-18 | **Ratified**: 2026-07-18 | **Last Amended**: 2026-07-18
