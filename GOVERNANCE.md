# Governance

This document describes how the gq project is run and who has access to what.
gq is an open source project and welcomes new contributors; the roles below
describe how responsibility and access are earned and granted.

## Roles and Responsibilities

### Contributors

Anyone who submits issues, pull requests, documentation, or reviews.
Contributions are made via GitHub pull requests and must follow
[CONTRIBUTING.md](CONTRIBUTING.md). Contributors have no direct access to
sensitive resources; their changes are merged by maintainers after CI passes.

### Maintainers

Maintainers are responsible for:

- Reviewing and merging pull requests.
- Triaging issues and security reports (see [SECURITY.md](SECURITY.md)).
- Cutting releases and managing release signing.
- Administering repository settings, branch rulesets, CI/CD, and secrets.

Regular contributors who demonstrate sustained, high-quality involvement and
sound judgment may be invited to become maintainers by the existing
maintainers. New maintainers are granted the minimum GitHub permission level
needed for their duties; repository administration remains with the project
owner unless explicitly delegated.

## Members with Access to Sensitive Resources

Sensitive resources include repository settings, branch rulesets, GitHub
Actions secrets (e.g. `CODECOV_TOKEN`), release publishing, and security
advisories.

Current members with such access:

| Member | Role | Access |
|---|---|---|
| [@javirub](https://github.com/javirub) | Maintainer / owner | Repository administration, secrets, releases, security advisories |

This table is updated whenever access is granted or revoked. Collaborator
access is always assigned manually and scoped to the lowest privilege
required.

## Decision Making

Day-to-day decisions are made by maintainers through pull request review.
Significant changes (breaking behavior, departures from yq parity, new
dependencies) should be discussed in a GitHub issue before implementation.
