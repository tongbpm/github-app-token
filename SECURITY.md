# Security policy

## Reporting a vulnerability

Please do not report security vulnerabilities in public issues. Use
GitHub's private vulnerability reporting for this repository when available.
If it is unavailable, contact the repository owner privately through GitHub
with enough detail to reproduce the issue.

## Credential handling

This package handles GitHub App private keys and installation tokens, which
are bearer credentials. Do not commit, log, print, or persist them. Prefer
mounted secret files or a secret manager, and use the smallest practical
process and filesystem permissions.
