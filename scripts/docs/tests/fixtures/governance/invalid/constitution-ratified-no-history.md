# XDev Constitution (Test Fixture - Ratified with No Ratified Row)

## Document Status

| Field | Value |
|---|---|
| Project | XDev |
| Version | `1.0.0` |
| State | Ratified |
| Candidate Date | 2026-07-15 |
| Ratified Date | 2026-07-16 |
| Approver | `xinyan <zuijiuhjd@foxmail.com>` |

This fixture tests that a Ratified state with a valid date but no Ratified status row
in Approval History is rejected. The Approval History only has "Candidate Draft Authorized"
and "Pending" rows — no "Ratified" in the Status column.

## Non-Negotiable Principles

### I. Security and Trust Boundary

Test fixture principle content. No placeholders.

### II. Tests Must Isolate Host State

Test fixture principle content.

## Approval History

| Phase | Status | Approved By | Date | Doc Version | Approval Scope |
|---|---|---|---|---|---|
| Constitution Structure | Candidate Draft Authorized | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-15 | `1.0.0-rc.1` | Test fixture |
| Constitution Ratification | Pending | `xinyan <zuijiuhjd@foxmail.com>` | 2026-07-16 | `1.0.0` | Pending ratification |

**Version**: `1.0.0` | **Candidate Date**: 2026-07-15 | **Ratified**: 2026-07-16 | **Last Amended**: 2026-07-16
