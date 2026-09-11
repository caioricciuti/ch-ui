# Bundled fonts

Self-hosted so CH-UI looks the same offline and behind firewalls, and so no
request leaves the deployment for a font. Both are licensed under the SIL
Open Font License 1.1 (license texts alongside).

| File | Source | Archive SHA-256 |
|---|---|---|
| InterVariable.woff2 | https://github.com/rsms/inter/releases/download/v4.1/Inter-4.1.zip (`web/InterVariable.woff2`) | 9883fdd4a49d4fb66bd8177ba6625ef9a64aa45899767dde3d36aa425756b11e |
| JetBrainsMono-Regular.woff2, JetBrainsMono-Medium.woff2 | https://github.com/JetBrains/JetBrainsMono/releases/download/v2.304/JetBrainsMono-2.304.zip (`fonts/webfonts/`) | 6f6376c6ed2960ea8a963cd7387ec9d76e3f629125bc33d1fdcd7eb7012f7bbf |

File SHA-256:

    693b77d4f32ee9b8bfc995589b5fad5e99adf2832738661f5402f9978429a8e3  InterVariable.woff2
    086c48dfbea9ddaff1320f7e09399b8e2924e88ce67453721255db3bdbb5a353  JetBrainsMono-Medium.woff2
    a9cb1cd82332b23a47e3a1239d25d13c86d16c4220695e34b243effa999f45f2  JetBrainsMono-Regular.woff2

Downloaded 2026-09-10 from the official release archives; nothing was
modified. Both archive hashes were cross-checked against Homebrew's cask
records for the same releases (`formulae.brew.sh/api/cask/font-inter.json`
and `font-jetbrains-mono.json`) and matched byte for byte; the GitHub
release assets show a single upload (created == updated) on 2024-11-16 and
2023-01-14 respectively. To upgrade, replace the files, repeat the
cross-check, and update this table.
