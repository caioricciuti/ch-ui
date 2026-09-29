# Security Policy

We take the security of CH-UI seriously. CH-UI is a self-hosted application that
sits next to production ClickHouse deployments, so we treat security reports as a
priority.

## Supported versions

Security fixes are provided for the latest minor release line. We recommend always
running the most recent release.

| Version | Supported          |
| ------- | ------------------ |
| 2.13.x  | :white_check_mark: |
| < 2.13  | :x:                |

## Reporting a vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Report privately using either of the following:

- **GitHub Security Advisories** — use the "Report a vulnerability" button under the
  repository's **Security** tab (preferred; gives us a private, coordinated channel).
- **Email** — me@caioricciuti.com

Please include:

- A description of the vulnerability and its impact
- Steps to reproduce (proof-of-concept if possible)
- Affected version(s) and configuration
- Any suggested remediation

## What to expect

- **Acknowledgement** within 3 business days.
- An initial assessment and severity classification within 7 business days.
- Coordinated disclosure: we will agree on a disclosure timeline with you and credit
  you in the release notes unless you prefer to remain anonymous.

## Scope

In scope: the CH-UI server binary, the connector agent, the web UI, and the release
artifacts published from this repository.

Out of scope: vulnerabilities in ClickHouse itself, in third-party LLM/email
providers you configure, or issues that require a pre-compromised host or admin
credentials.
