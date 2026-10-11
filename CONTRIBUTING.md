# Contributing to CH-UI

Thanks for helping. A few rules keep this manageable for one maintainer.

## Bugs go in issues, questions in discussions

- **Found a bug?** [Open an issue](https://github.com/caioricciuti/ch-ui/issues/new/choose)
  with the bug report form. Include the CH-UI version (`ch-ui version`), the
  ClickHouse version, how you run it (binary, Docker, Helm), and the smallest
  steps that reproduce it.
- **Want a feature?** Open an issue with the feature request form.
- **Have a question or an idea to talk through?** Use
  [Discussions](https://github.com/caioricciuti/ch-ui/discussions).
- **Found a security problem?** Do not open an issue. Follow
  [SECURITY.md](SECURITY.md).

Docs live at [ch-ui.com/docs](https://ch-ui.com/docs). Many "can't log in"
problems are covered in [Can't login?](https://ch-ui.com/docs/cant-login/).

## Pull requests

1. **Sign the CLA.** On your first pull request a bot asks you to sign the
   [Contributor License Agreement](CLA.md) by commenting
   `I have read the CLA Document and I hereby sign the CLA`. You do it once and
   it covers all your future pull requests. Pull requests cannot be merged
   until the `CLA` check passes. CH-UI is dual-licensed (see
   [LICENSING.md](LICENSING.md)), and the CLA lets the project ship your work
   under either license. You keep the copyright.
2. **Open an issue first for anything bigger than a small fix**, so we can
   agree on the approach before you spend time on it.
3. **Keep it small and focused.** One change per pull request. No unrelated
   formatting or refactoring.
4. **No new dependencies** unless agreed in the issue first. Dependency
   changes get a security review before merge.
5. **Run the checks** for what you touched:
   - Go: `make vet test`
   - UI: `cd ui && bun install --frozen-lockfile && bun run check && bun run test`
   - Site and docs: `cd site && bun install --frozen-lockfile && bun run build`
6. **Update the docs** in `site/src/content/docs/docs/` when you change
   something users see, and add a line under `## [Unreleased]` in
   [CHANGELOG.md](CHANGELOG.md).
7. **Commit messages** follow [Conventional Commits](https://www.conventionalcommits.org/)
   (`fix(query): ...`, `feat(cli): ...`).

## Licensing of contributions

Files under the Pro paths listed in [LICENSING.md](LICENSING.md) are under the
Business Source License 1.1; everything else is Apache 2.0, except `site/`,
which is not open source. By signing the CLA you agree that your contribution
can be distributed under these licenses and others the maintainer chooses.
