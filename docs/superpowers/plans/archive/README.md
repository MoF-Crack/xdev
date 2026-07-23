# XDev Plan Archive

This directory contains superseded or completed plans that have been formally
archived after reaching Accepted status and receiving Gate C authorization.

## Archive Specification

1. **Structure:** `archive/<year>/<original-filename>.md`
2. **Annual Index:** `archive/<year>/README.md` with a table of archived plans
3. **Archive Condition:** Plan status must be Accepted and Gate C must be
   explicitly authorized by the user
4. **No Back-Writes:** Once archived, the file is immutable. Corrections use
   `<original-filename>.errata.md` in the same directory
5. **Link Updates:** All references to the original path are updated to the
   archive path at archive time
6. **Approval Boundary:** The approval gate does NOT protect
   `docs/superpowers/plans/` or its archive subpaths. Plan authorization relies
   on in-document Approval History + explicit user authorization, not on
   `.specify/approvals/*.json`

## Annual Indexes

- [2026](2026/README.md) -- Plans archived in 2026
