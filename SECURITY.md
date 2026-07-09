# Security Policy

## Reporting a Vulnerability

Please **do not report security vulnerabilities through public GitHub issues**.

Instead, use GitHub's private vulnerability reporting:

1. Go to the [Security tab](https://github.com/javirub/gq/security) of this repository.
2. Click **"Report a vulnerability"** and fill in the advisory form.

You can also reach the maintainer directly via the contact information on the
[maintainer's GitHub profile](https://github.com/javirub).

When reporting, please include:

- A description of the vulnerability and its potential impact.
- Steps to reproduce, ideally with a minimal input file and the exact `gq` command used.
- The affected version (`gq --version`) and platform.

## What to Expect

- An acknowledgment of your report within **7 days**.
- A status update once the issue has been triaged, including whether it is accepted as a vulnerability.
- Credit in the release notes and security advisory when the fix is published, unless you prefer to remain anonymous.

## Supported Versions

Only the **latest release** receives security fixes; older releases stop
receiving security updates as soon as a newer release is published. Please
update to the most recent version before reporting an issue.

## Vulnerability Remediation Policy

Dependency and static-analysis findings are handled as follows:

- **Dependency vulnerabilities (SCA)**: every change is automatically checked
  by `govulncheck` in CI (blocking) and the repository is continuously
  monitored by Dependabot security updates. Findings that affect gq's calling
  code are remediated before the next release; critical/high findings within
  **14 days**, medium/low within **90 days**. Findings in dependencies that do
  not affect gq (unreachable code paths per govulncheck) are documented in the
  suppressing PR.
- **VEX**: dependency vulnerabilities that do not affect gq (unreachable per
  govulncheck's call-graph analysis) are published as an OpenVEX document in
  [`security/openvex.json`](security/openvex.json), regenerated with
  `make vex` whenever new advisories appear and before each release.
- **Licenses**: dependencies must be MIT, BSD, or Apache-2.0 licensed;
  violations block the dependency from being added.
- **Static analysis (SAST)**: CodeQL and OpenSSF Scorecard run on every pull
  request and on a weekly schedule. New CodeQL alerts of severity high or
  above must be fixed or formally dismissed as false positive/non-exploitable
  before merging; lower-severity alerts within **90 days**.
- No release is published with known unaddressed critical or high findings.

## Secrets Management

The project itself holds no user secrets. Operational credentials
(e.g. `CODECOV_TOKEN`) are stored exclusively as GitHub Actions encrypted
secrets, never in the repository (GitHub push protection is enabled and
blocks accidental commits of credentials). Access is limited to maintainers
listed in [GOVERNANCE.md](GOVERNANCE.md); workflows run with least-privilege
permissions. Secrets are rotated when a maintainer with access leaves or on
any suspicion of exposure.
