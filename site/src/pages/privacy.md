---
layout: ../layouts/Legal.astro
title: Privacy Policy
description: "How CH-UI handles your data: this website, self-hosted CH-UI, and the license service at license.ch-ui.com. Self-hosted CH-UI keeps everything local."
effective: September 30, 2026
updated: September 30, 2026
---

This policy explains what personal data CH-UI handles, why, on what legal basis, who else sees it, and for how long. It covers this **website and documentation** (ch-ui.com), the **license service** at license.ch-ui.com, and the CH-UI software you **self-host**.

## Who is responsible

The controller is **Caio Ricciuti**, a sole trader established in Spain, who operates CH-UI ("we", "us"). Contact for anything in this policy: [me@caioricciuti.com](mailto:me@caioricciuti.com). The operator's tax ID and the other details Spanish law requires are in the [legal notice](/legal/).

## Visiting this website

- **Hosting and request data.** This website is a set of static pages served by **Cloudflare**. To deliver and protect it, Cloudflare processes each request: IP address, user agent, requested URL and time. We keep no access logs of our own for this website. Legal basis: our legitimate interest in running a secure service (Art. 6(1)(f) GDPR).
- **Analytics.** We count visits with **Umami**, self-hosted on our own server. It sets **no cookies**, does not store your IP address, and ignores URL query strings. It records the page, referrer, browser, device type, screen size, country, and a few anonymous events such as "form submitted", and shows us aggregate statistics only. Nothing is shared with a third party. Legal basis: legitimate interest in understanding how the site is used (Art. 6(1)(f)).
- **GitHub star count.** Some pages show the project's GitHub star count. Your browser fetches it directly from GitHub's public API (api.github.com), so GitHub, Inc. (USA) receives your IP address and user agent under its own privacy policy. The count is cached in your browser for six hours.
- **Cookies and local storage.** This site sets no cookies. Your browser's local storage may hold your light/dark theme choice and the cached star count; they stay on your device and are not sent to us.

## Newsletter

If you subscribe, we store your email address in a mailing list hosted by **Resend** and send you release notes and tips. Legal basis: your consent (Art. 6(1)(a)), which you can withdraw at any time with the unsubscribe link in any newsletter or by emailing us. We keep your address until you unsubscribe. Sign-ups are rate-limited by IP address; the counter is kept by Cloudflare for about a minute and we never store the address.

## Trials and purchases (license.ch-ui.com)

- **Trials.** We store your email address, the name you give (optional), and the trial license issued to you. To keep trials to one per person we also store a normalized form of your address and check that its domain can receive email and is not a disposable-email service.
- **Purchases.** Payment is handled by **Stripe**, which collects your payment and billing details; we never see or store your full card number. From Stripe we receive your name, email address, and Stripe customer and subscription identifiers. We store those together with the licenses issued to you, their dates and status.
- **The license file.** Your name (or, if you give none, your email address) is written into the signed license file, which is how CH-UI shows who a license belongs to.
- **Email.** License files, renewals and billing-portal links are sent through **Resend**. We also get a short notification email about trials, purchases, renewals, cancellations, refunds and disputes.
- **Server logs.** The license server records each request: IP address, user agent, requested URL and time. Request traces (IP address, user agent, path) are deleted automatically after 30 days; access logs are rotated out by size. We use them to keep the service secure and working. Legal basis: our legitimate interest in running a secure service (Art. 6(1)(f)).
- **Abuse protection.** Requests are rate-limited by IP address, held in memory for up to an hour and never stored.
- **Legal bases.** Issuing, delivering and renewing your license: performance of our contract with you, or steps you asked for before one (Art. 6(1)(b)). Keeping purchase records: our legal obligations under tax and accounting law (Art. 6(1)(c)). One-trial-per-person checks, rate limits and payment alerts: our legitimate interest in preventing abuse and fraud (Art. 6(1)(f)).

## How long we keep it

- **License and purchase records:** for the life of the license, then for as long as Spanish tax and accounting law requires us to keep purchase records (generally up to six years).
- **Trial records:** kept so a trial can only be taken once per person; we delete them on request.
- **Newsletter:** until you unsubscribe.
- **Analytics:** at most 25 months.
- **Server logs and traces:** as described above.
- **Backups:** the license database is backed up nightly and each backup is kept for up to 14 days, so deleted data can remain in backups for that long.

## Who else processes it

We do not sell your data or use it for advertising. These providers process it on our behalf, each only for its function:

- **Cloudflare**: serves this website from its network and answers DNS for the domain.
- **Hetzner Online**: hosting of the license service, the analytics and their logs, on servers in Helsinki, Finland (EU).
- **Stripe**: payment processing, subscriptions and the billing portal.
- **Resend**: email delivery (license emails, owner notifications) and the newsletter list.

Cloudflare, Stripe and Resend may process data in the United States. For those transfers we rely on the EU-US Data Privacy Framework or on the European Commission's Standard Contractual Clauses, as provided in each provider's data processing terms.

## Self-hosted CH-UI

CH-UI runs on your own infrastructure. When you self-host it, whether the open-source core or Pro:

- **No telemetry, analytics, or usage data** is sent to us or any third party. We receive no data from your CH-UI deployment.
- All application state is stored in a local **SQLite** database on your server. Your queries, governance metadata, and database contents stay on your infrastructure.
- Pro licenses are **signed files verified offline**; your deployment does not need to contact our servers to validate a license.
- It connects to **your ClickHouse**; to your chosen AI provider only if you enable Brain with your own key; and to the license service only when you ask it to, for example to request a trial from inside the app.
- It contacts **GitHub** (GitHub, Inc., USA) to check for new releases: the web app asks api.github.com for the latest release from each user's browser, and `ch-ui update` downloads release files from GitHub when you run it. If you set up GitHub model sync, it also talks to the repository you configure, with the token you provide.
- It sends email only through the **email providers you configure** for alerts and reports: your own SMTP server, Resend, or Brevo.
- It forwards audit events to a **webhook** only if you configure one.
- It contacts your **identity provider** only if you configure OIDC single sign-on.

## Your rights

- You can ask for access to, correction or deletion of your data, restriction of its use, a portable copy, or object to processing based on our legitimate interests. Where we rely on your consent, you can withdraw it at any time.
- Email us at the address above; we reply within one month. We may need to keep purchase records that tax law requires us to hold.
- You can complain to the Spanish data protection authority, the [Agencia Española de Protección de Datos (AEPD)](https://www.aepd.es), or to the authority where you live.
- We make no decisions about you based solely on automated processing that have legal or similarly significant effects.

## Security

All traffic to the website and license service is encrypted in transit over TLS. We keep only what is needed to issue and support licenses, and payment details stay with Stripe.

## Changes

We may update this policy. Material changes will be posted on this page with a new "last updated" date.
