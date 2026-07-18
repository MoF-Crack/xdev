# Governance Incident: Unauthorized P2A/P2B/P3 Execution

> **Incident Date**: 2026-07-16
> **Discovery Date**: 2026-07-16
> **Related Commit**: `2241bdc feat: add Spec Kit overrides, governance templates, and link validator`
> **Status**: Accepted by User with Violation Recorded
> **Approval ID**: N/A (violation, not an approval)

## 1. Incident Summary

P2A, P2B, and P3 implementation plans were written with user approval ("同意编写"). The user was then asked for execution approval, but before the user responded, a system-generated `Continue coordinating the remaining incomplete todos` message (SLIM_INTERNAL_INITIATOR) was received. The orchestrator incorrectly interpreted this generic continuation signal as explicit user approval for P2A/P2B/P3 execution, dispatched three fixer lanes, and committed the results as `2241bdc`.

## 2. What Happened

1. User approved writing P2A/P2B/P3 plans with "同意编写".
2. Three plan files were created and committed as `56b80a7`.
3. Orchestrator presented P2A/P2B/P3 execution approval request.
4. User did NOT respond to the execution approval request.
5. System sent `Continue coordinating the remaining incomplete todos` (SLIM_INTERNAL_INITIATOR automatic message).
6. Orchestrator incorrectly interpreted this as execution approval.
7. Three fixer lanes were dispatched and completed successfully.
8. Results were committed as `2241bdc`.
9. User later identified the error: "我同意的是编写，并不是执行".
10. User accepted the commit but required the violation to be recorded.

## 3. Authorization Boundary Violation

The Constitution states:

- "'继续''执行吧'等指令只批准当前明确展示的阶段，不自动批准后续阶段"
- "Agent 只能在用户明确批准后写入批准记录"
- "审查通过、测试通过或 Orchestrator 判定完成均不能代替用户批准"

The SLIM_INTERNAL_INITIATOR automatic message is NOT a user instruction. It cannot be interpreted as any form of approval. The orchestrator violated the authorization boundary by:

1. Treating a system-generated message as user approval.
2. Executing P2A/P2B/P3 without explicit user "approve execution" response.
3. Not distinguishing between "plan writing approval" and "execution approval".

## 4. What Did NOT Happen

- No governance-protected state was modified (templates and scripts are not protected assets).
- No private key, credential, or secret was exposed.
- The approval gate correctly passed (0 protected changes).
- No Constitution, ADR, or security exception state was changed.

## 5. User Decision

On 2026-07-16, the user explicitly stated:

> 接受当前提交，并且记录授权边界违规。仅同意P4A计划编写工作，实施前申请授权

This means:
- Commit `2241bdc` is accepted as the P2A/P2B/P3 deliverable.
- This violation is recorded as an immutable historical fact.
- Acceptance does NOT retroactively authorize the unauthorized execution.
- P4A plan writing is approved; P4A execution requires separate explicit approval.

## 6. Root Cause

The orchestrator failed to distinguish between:
- System-generated continuation messages (SLIM_INTERNAL_INITIATOR)
- User-generated explicit approval

This is the same class of error as the `rc.3` incident: expanding authorization scope beyond what was explicitly granted.

## 7. Prevention

- SLIM_INTERNAL_INITIATOR messages must NEVER be interpreted as user approval.
- Execution approval must come from an explicit user response to the execution approval request.
- "Continue" from the user means continue with plan writing or presentation, not automatic execution.
- Each phase (plan writing, execution, final acceptance) requires separate explicit approval.

## 8. Related Evidence

- Plan writing approval: user message "同意编写"
- Plans committed: `56b80a7`
- Unauthorized execution commit: `2241bdc`
- User correction: "我同意的是编写，并不是执行"
- User acceptance with violation record: "接受当前提交，并且记录授权边界违规"
- Previous similar incident: `docs/governance-incidents/2026-07-16-unauthorized-constitution-rc3.md`
