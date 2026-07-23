# Governance Stabilization Plan Errata

> **Document type:** Errata
> **Status:** Active Errata
> **Created:** 2026-07-23
> **Applies to:** [2026-07-18-governance-stabilization.md](2026-07-18-governance-stabilization.md)

## Authoritative Current State

The archived plan records the full execution history, including intermediate
LOOP states. Statements in those historical sections such as "Task 8 pending",
"Task 9-10 not executed", and older assertion totals do not describe the final
state.

- The document state is `Archived`.
- Tasks 1-10 completed in the local working tree.
- The current Task 8 result is `157 assertions PASS`; `153` and earlier totals
  are superseded.
- Approval tests completed at `25/25 PASS`.
- Governance cross-document tests completed at `28/28 PASS`.
- Task 6's explicitly approved scope extension included
  `scripts/docs/validate-approvals.ps1` and its tests. Earlier text describing
  those files as unchanged is superseded by that approval.

## Git Repository State

At the time of this correction, `HEAD` is `e91310c`. The governance
stabilization implementation and `docs/superpowers/plans/archive/` are present
only in the local working tree and are not tracked or committed. Therefore,
`Archived` describes the document workflow state; it does not prove that the
archive or a `git mv` is present in `HEAD` or in remote history.

The repository has no configured remote. GitHub Actions Windows runner
behavior, `actions/checkout`, and `actions/upload-artifact` remain unverified.

## Recovery Order

1. Read this errata before using the archived plan as a recovery source.
2. Run `git status --short --branch` and `git log --oneline -5`.
3. Re-run the focused governance tests and local CI simulation.
4. Obtain explicit authorization before staging or committing.
5. Stage only the approved file whitelist and verify the staged diff.

Never use `git add .` or `git add -A`. Never stage or commit
`keys/priv.key`.

## Correction Policy

The archived plan is immutable. Future corrections must be appended to this
errata file or a later errata document; do not edit the archived plan body.
