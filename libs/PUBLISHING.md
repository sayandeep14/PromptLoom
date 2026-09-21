# Publishing the client libraries

Three libraries talk to LoomLocker: **bloompy** (Python → PyPI), **loomj** (Java → Maven Central)
and **gloom** (Go → the Go module proxy). Publishing is done by
[`.github/workflows/release-libs.yml`](../.github/workflows/release-libs.yml) when you push a tag.
Everything is verified (tests, packaging, tag = version) on every pull request; only the upload
needs the one-time account setup below.

## Releasing

| Library | Bump the version in | Tag |
|---|---|---|
| bloompy | `libs/bloompy/pyproject.toml` | `bloompy-vX.Y.Z` |
| loomj | `libs/loomj/pom.xml` | `loomj-vX.Y.Z` |
| gloom | (the tag *is* the version) | `libs/gloom/vX.Y.Z` |

```bash
git tag bloompy-v0.1.0 && git push origin bloompy-v0.1.0
```

The workflow refuses a tag that does not match the version in the file. Run it from the *Actions*
tab (*Run workflow*) to build and check everything without publishing.

## One-time setup

### PyPI (bloompy) — no token needed

1. Create the project name on <https://pypi.org> (register an account, enable 2FA).
2. **Publishing → Add a pending publisher**: owner `sayandeep14`, repository `PromptLoom`,
   workflow `release-libs.yml`, environment `pypi`.
3. In the GitHub repository: **Settings → Environments → New environment** named `pypi`.

PyPI then accepts uploads from that workflow using short-lived OIDC credentials; no secret is stored.

### Maven Central (loomj)

1. Sign in at <https://central.sonatype.com> and **verify a namespace**.
   - `dev.promptloom` (the current `groupId`) requires you to prove you control the domain
     `promptloom.dev` (a DNS TXT record).
   - If you do not own that domain, use `io.github.sayandeep14`, which is verified by creating a
     temporary GitHub repository. That means changing `<groupId>` in `pom.xml`, the Java package
     names and the README; do it *before* the first release, because coordinates cannot change after.
2. Generate a **user token** (Account → Generate User Token).
3. Create a GPG key and publish its public half:
   `gpg --full-generate-key`, then `gpg --keyserver keyserver.ubuntu.com --send-keys <KEYID>`.
   Export the private key for the workflow: `gpg --armor --export-secret-keys <KEYID>`.
4. In GitHub: **Settings → Environments → New environment** `maven-central`, with secrets
   `MAVEN_CENTRAL_USERNAME`, `MAVEN_CENTRAL_PASSWORD` (the token pair), `GPG_PRIVATE_KEY`,
   `GPG_PASSPHRASE`.

`autoPublish` is off: the workflow uploads the signed bundle and you press **Publish** on
central.sonatype.com after checking it. Turn `autoPublish` on in `pom.xml` once you trust the flow.

### Go (gloom)

Nothing to set up. `go get github.com/sayandeep14/PromptLoom/libs/gloom@v0.1.0` works as soon as
the `libs/gloom/v0.1.0` tag exists.
