# Rasterklang Contribution Triage

Use this playbook to turn incoming issues and pull requests into clear, releasable work. Keep the public tracker honest: unsupported features should be labeled as future work instead of described as available.

## First Response

- Add `status: needs triage` until the report has an owner, area, type, and priority.
- Ask for the smallest reproduction that still shows the problem.
- Link related release-plan items, changelog entries, or known support limits.
- Move security-sensitive reports out of public issues and into the process in `SECURITY.md`.

## Bug Reports

A bug report is ready when it includes:

- Rasterklang component and version.
- Operating system, browser, or package channel when relevant.
- Input tune or synthetic fixture that can be legally shared.
- Expected behavior, actual behavior, and whether the issue is deterministic.
- Logs, command output, or screenshots when they clarify the failure.

Use `status: needs reproduction` when the problem is plausible but not yet actionable.

## Feature Requests

A feature request is ready when it states:

- The user workflow it enables.
- The component that should own it.
- Whether it changes public API, file format, release artifacts, or website claims.
- Any compatibility or licensing risks.

Do not label work `help wanted` until the expected behavior and acceptance checks are specific.

## Release Work

Use `type: release` for packaging, provenance, signing, CI, versioning, public repo identity, package-manager publication, and deployment tasks. Release work must name the verification command or external evidence that closes it.

## Security Reports

Public issues should not include exploit details, private credentials, or unreleased vulnerability analysis. A public tracker item can exist only after sensitive details are handled through `SECURITY.md`.

## Label Policy

Every issue should have:

- One `type: *` label.
- At least one `area: *` label.
- One `priority: p*` label after triage.
- A `status: *` label when the issue is not ready for implementation.

Use `good first issue` only for tasks that can be completed without private assets, unpublished credentials, or deep release context.

## Definition Of Ready

An issue is ready for implementation when:

- Expected behavior is concrete.
- Acceptance checks are listed.
- Required fixtures or legal sample files are available.
- Ownership across core, WASM, desktop, webplayer, and website is clear.
- External blockers are either resolved or labeled.

## Close Criteria

Close an issue only when the implementation, tests or manual verification, documentation, and release-plan updates match the stated acceptance checks. If the issue is intentionally deferred, close it with the support boundary or future milestone stated explicitly.
