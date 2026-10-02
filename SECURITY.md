# Security policy

Please report suspected vulnerabilities privately through
[GitHub Security Advisories](https://github.com/deepnoodle-ai/decide/security/advisories/new).
Do not open a public issue for a vulnerability before we have looked at it.

Include the version or commit, the impact, the steps to reproduce it, and a
proof of concept if you have one.

## Scope

This policy covers the Go packages and the `decide` command in this
repository. Before v1, fixes go into the latest release only.

Examples of what we want to hear about:

- Credentials appearing in errors, logs, saved runs, or output.
- File names, data, or skill files that can send escape sequences to the
  terminal.
- Reading or writing files the command was not asked to touch.
- Answers accepted without being validated against their questions.

Problems with the TypeSafe or Cloudflare services themselves belong with
those providers.
