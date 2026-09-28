---
schema: 4
id: TKT-01M3K5CV7G7SMEB6V0MBZFT42K
title: Run Terva reviews from the v0.5.0 reviewer image
type: chore
status: done
status_reason: null
priority: normal
due_on: null
labels:
  - area/ci
assignees: []
milestone: null
parent: null
origin: null
dependencies: []
blocks_on: none
references: []
moved_to: null
claim: null
archive: null
created_at: 2026-09-28T04:46:58Z
updated_at: 2026-09-28T04:49:37Z
created_by:
  id: agent:claude/t3code-45332409
  name: ""
updated_by:
  id: agent:claude/t3code-45332409
  name: ""
extensions: {}
---

## Description

Move this repository's Terva review from the terva-action-code-review v0.4.0 image to v0.5.0 (https://git.local.sothr.com/terva-sh/terva-action-code-review/releases/tag/v0.5.0), pinned by digest `sha256:64a7ba59ca8a0932e2b4d4cf8e50a2231f46fa6eaefa046acbef84f2a2886252`.

### What it gains

- **Up to 1 MiB of context,** not a fixed 256 KiB, so a large PR is reviewed rather than stopped at `context_limit`.
- **A change index** in every prompt.
- **Fallback models** from `TERVA_REVIEW_FALLBACKS`, now read by the workflow.
- **Decided findings** reach the reviewer.
- **A carry of every gate** in one dispatch.

Every profile digest and request key moves, so the first review of each open PR after this merges runs fresh. The allowlist, the concurrency group and the other settings stay as they are.

Part of terva-action-code-review TKT-01M3K5B2YX ('Move the consumers to the v0.5.0 image').

## Acceptance criteria

- [x] The review workflow runs the v0.5.0 image by digest and reads TERVA_REVIEW_FALLBACKS
- [x] A review of this change from its branch is recorded

## Notes

**agent:claude/t3code-45332409** at 2026-09-28T04:49:36Z

PR #15 (https://git.local.sothr.com/terva-sh/tuohi/pulls/15), head 51ea24a. Terva review from the PR's own branch, so the v0.5.0 image reviewed its own installation: request review-v0.5.0, run 57ec13d8-ac9b-4c61-a653-a40266adb0e7, terva-review/code success with no findings. Merge authorized by the maintainer on 2026-09-28.

## Summary

The Terva review runs terva-action-code-review v0.5.0 (sha256:64a7ba59, commit f3a857b) and reads TERVA_REVIEW_FALLBACKS, shipped in #15. Its branch review was clean.
