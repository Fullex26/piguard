# Support

## Where to Get Help

- **Questions / setup help**: open a GitHub Discussion (preferred) or a GitHub issue if Discussions are unavailable.
- **Bug reports**: use the Bug Report issue template.
- **Feature requests**: use the Feature Request issue template.
- **Security vulnerabilities**: **do not** file public issues; follow `SECURITY.md`.

## Before Opening an Issue

Please include:

1. Pi model and OS (`uname -a`)
2. PiGuard version (`piguard version`)
3. Config context (redact secrets)
4. Relevant logs (`journalctl -u piguard -n 200`)
5. Steps to reproduce and expected vs actual behavior

## Support Boundaries

- The project supports the **latest tagged release**.
- Help for older versions is best effort only.
- Custom downstream modifications are out of scope unless reproducible on upstream `main` or a tagged release.
