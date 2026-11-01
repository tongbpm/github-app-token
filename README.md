# github-app-token

[![CI](https://github.com/tongbpm/github-app-token/actions/workflows/run-tests.yaml/badge.svg)](https://github.com/tongbpm/github-app-token/actions/workflows/run-tests.yaml)
[![Go Reference](https://pkg.go.dev/badge/github.com/tongbpm/github-app-token.svg)](https://pkg.go.dev/github.com/tongbpm/github-app-token)
[![Go Report Card](https://goreportcard.com/badge/github.com/tongbpm/github-app-token)](https://goreportcard.com/report/github.com/tongbpm/github-app-token)
[![Latest Release](https://img.shields.io/github/v/release/tongbpm/github-app-token)](https://github.com/tongbpm/github-app-token/releases)
[![License](https://img.shields.io/github/license/tongbpm/github-app-token)](./LICENSE)

> **AI-assisted code:** This project was developed with assistance from
> GitHub Copilot. Review and validate it before using it in production.

`github-app-token` is a focused Go package for creating and caching GitHub App
installation access tokens, with an optional CLI for local use and CI jobs.
It handles App JWT signing, installation-token exchange, in-memory caching,
and expiry-aware refresh without becoming a general GitHub API client.

## Install

Import the library:

```sh
go get github.com/tongbpm/github-app-token
```

Install the CLI:

```sh
go install github.com/tongbpm/github-app-token/cmd/github-app-token@latest
```

Use the library for one-off token requests, or use `githubapptoken.Client` for
repeated requests so tokens are reused and refreshed automatically.

## Usage

```sh
github-app-token \
  --app-id 123456 \
  --installation-id 12345678 \
  --private-key ./github-app.pem
```

The private key can also be supplied directly with `--private-key-pem`, or
through environment variables:

```sh
export GITHUB_APP_ID=123456
export GITHUB_INSTALLATION_ID=12345678
export GITHUB_PRIVATE_KEY="$(
  cat ./github-app.pem
)"
github-app-token
```

`GITHUB_PRIVATE_KEY` accepts either PEM contents or a path to a PEM file.
The key must be an RSA private key. The command uses the GitHub REST API at
`https://api.github.com` by default; `--api-url` can be used with a GitHub
Enterprise Server URL. Use `--timeout` to change the HTTP request timeout.

The generated app JWT is never printed. Only the installation access token is
written to standard output; errors are written to standard error. Treat the
private key and returned installation token as secrets and never commit or
log them.

## Go package

Import the module as `githubapptoken`:

```go
package main

import (
    "context"
    "log"

    githubapptoken "github.com/tongbpm/github-app-token"
)

func main() {
    key, err := githubapptoken.ParsePrivateKey("/run/secrets/github-app.pem")
    if err != nil {
        log.Fatal(err)
    }

    token, err := githubapptoken.CreateInstallationToken(context.Background(), githubapptoken.TokenRequest{
        AppID:          "123456",
        InstallationID: "12345678",
        PrivateKey:     key,
    })
    if err != nil {
        log.Fatal(err)
    }
    _ = token
}
```

`TokenRequest.APIURL` defaults to `https://api.github.com`. Set it for GitHub
Enterprise Server. `TokenRequest.HTTPClient` can be provided for custom
transports, proxies, or test servers. `TokenRequest.Timeout` applies when the
library creates its default HTTP client.

For applications that request tokens repeatedly, use `Client` to cache the
token in memory and refresh it one minute before GitHub reports that it
expires:

```go
client, err := githubapptoken.NewClient(githubapptoken.ClientConfig{
    TokenRequest: githubapptoken.TokenRequest{
        AppID:          "123456",
        InstallationID: "12345678",
        PrivateKey:     key,
    },
})
if err != nil {
    log.Fatal(err)
}

token, err := client.Token(context.Background())
```

Concurrent callers share one refresh request. Use `client.Refresh(ctx)` to
force a new token after an authentication failure, or `client.Invalidate()`
to make the next call refresh. The cache is process-local and never writes
tokens to disk. Cached mode rejects responses whose token expires within the
refresh window.

## Releases and dependency updates

Releases are created by pushing a semantic-version tag such as `v0.3.0`:

```sh
git tag v0.3.0
git push origin v0.3.0
```

You can also use the **Release** workflow’s **Run workflow** button in GitHub
Actions and enter the version. It validates and creates the tag automatically,
then builds and publishes the release.

The tag starts the [GoReleaser workflow](./.github/workflows/release.yml),
which builds the CLI for Linux, macOS, and Windows on amd64, arm64, and
arm where supported, then publishes the archives and checksums to a GitHub
Release. The Go package is installed from:

```sh
go install github.com/tongbpm/github-app-token/cmd/github-app-token@v0.2.0
```

[Dependabot](./.github/dependabot.yml) checks Go modules and GitHub Actions
weekly and opens update pull requests.

## Support and security

See [CONTRIBUTING.md](./CONTRIBUTING.md) for development and pull request
guidance. See [SECURITY.md](./SECURITY.md) for credential handling and
vulnerability reporting.

The package currently supports Go 1.23 and newer. The public API is still
pre-1.0 and may change between minor releases.
