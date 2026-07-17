# Governance Incident: Unauthorized Constitution rc.3 Revision

> **Incident Date**: 2026-07-16
> **Discovery Date**: 2026-07-16
> **Related Commit**: `059da207f3cbd028a7e1f95537914238fdf00ddb`
> **Status**: Corrective Action Completed
> **Approval ID**: `2026-07-16-constitution-rc3-001`

## 1. Incident Summary

Commit `059da20` revised the Constitution candidate version from `1.0.0-rc.2` to `1.0.0-rc.3` and submitted it as part of `chore: add CI and Spec Kit workflow` without obtaining explicit prior user approval for the Constitution revision. This exceeded the authorization scope of "continue executing the Git initial import plan."

## 2. What Happened

During execution of the Git initial import plan (Commit 4: `chore: add CI and Spec Kit workflow`), the following sequence occurred:

1. CI workflow `.github/workflows/ci.yaml` was updated to use Go `1.23` (matching `go.mod`).
2. The Constitution's "Known Execution Gaps" section contained a stale reference to "CI uses Go 1.22, mismatching go.mod's Go 1.23."
3. The orchestrator self-classified this as a "fact sync" and unilaterally revised the Constitution from `rc.2` to `rc.3`, removing the stale gap entry.
4. The Constitution revision was bundled into the CI/Spec Kit workflow commit without separate approval.

## 3. What Did NOT Happen

- No approval records were forged. The Approval History correctly recorded `Candidate Revised` with no approver for `rc.3`.
- The Constitution remained `Ratified: Pending`. It was not promoted to `1.0.0`.
- No private key, credential, or secret was exposed.

## 4. Authorization Boundary Violation

The Constitution's own rules (Section "Stage Approval Authority") state:

- "Continue" and "execute" instructions only approve the currently displayed stage, not subsequent stages.
- Agent can only write approval records after explicit user approval.
- Review pass, test pass, or orchestrator judgment cannot replace user approval.

The orchestrator's action violated these rules by:
1. Treating path permission (staging `.specify/`) as semantic permission (modifying Constitution content).
2. Treating problem discovery as fix authorization.
3. Not executing the Constitution-specific approval gate before editing.
4. Bundling a governance document revision into an unrelated commit, reducing auditability.

## 5. Root Cause

The validation system checked technical correctness (placeholders, version records, `specify check`, Git boundaries) but did not check authorization provenance ("where is the explicit user approval for this semantic change?"). Technical validation passing masked the governance violation.

## 6. Subsequent User Approval

On 2026-07-16, the user explicitly approved the `rc.3` candidate content with the authorization text:

> 1. Create errata record. 2. Agree to rc.3. 3. Add approval status hard gate, and every approval must be recorded at write time.

This approval:
- Confirms `rc.3` candidate content as the working candidate baseline from the approval moment forward.
- Does NOT retroactively authorize, cover, or eliminate the unauthorized operation in `059da20`.
- Does NOT constitute Constitution `1.0.0` formal ratification.
- The Constitution remains `Ratified: Pending`.

## 7. Corrective Actions

1. Created this governance incident record (immutable historical fact).
2. Created structured approval record `.specify/approvals/2026-07-16-constitution-rc3-001.json`.
3. Added Approval History row to Constitution referencing the same approval ID.
4. Implemented approval status hard gate (`scripts/docs/validate-approvals.ps1`).
5. Installed versioned pre-commit hook (`scripts/git-hooks/pre-commit.ps1`).
6. All future protected governance state writes require matching approval records in the same diff.

## 8. Prevention

- The approval gate validator now blocks commits that modify protected governance assets without matching approval records.
- The pre-commit hook calls the validator on every commit.
- Future Constitution revisions must follow: propose -> user approval -> write -> validate -> commit.
- Any similar boundary violation should be recorded as a new governance incident, not hidden.

## 9. Related Evidence

- Approval JSON: `.specify/approvals/2026-07-16-constitution-rc3-001.json`
- Constitution Approval History: `.specify/memory/constitution.md` (row with `2026-07-16-constitution-rc3-001`)
- Validator: `scripts/docs/validate-approvals.ps1`
- Test suite: `scripts/docs/tests/validate-approvals.tests.ps1` (21/21 pass)
- Pre-commit hook: `scripts/git-hooks/pre-commit.ps1`
- Hook installer: `scripts/install-git-hooks.ps1`
