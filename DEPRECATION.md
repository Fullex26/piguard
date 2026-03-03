# Deprecation Policy

This policy applies to CLI flags, config keys, notifier payload fields, and user-facing behavior.

## Policy

1. **Announce first**: deprecations are announced in `CHANGELOG.md` and release notes.
2. **Keep compatibility**: deprecated behavior remains functional for at least **one minor release** when feasible.
3. **Warn clearly**: runtime or startup warnings are added for deprecated config/flags where possible.
4. **Remove deliberately**: removals are called out under a **Breaking Changes** section.

## Required PR Checklist for Deprecations

- Mark deprecated items in docs (`README.md`, `configs/default.yaml`, or config comments)
- Add migration guidance (old → new)
- Add tests for compatibility during the deprecation window
- Include planned removal version in PR description

## Urgent Security Exception

In rare security-critical cases, behavior may be removed immediately. If that happens, migration notes are still required in the same release notes.
