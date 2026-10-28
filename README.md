# github-app-token

`github-app-token` creates a GitHub App installation access token. It is
available as a small Go package, with an optional CLI for local use and CI
jobs where the token should be written to standard output.

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
written to standard output; errors are written to standard error.

## Go package

The module can also be imported as `githubapptoken`:

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
    log.Println(token)
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

Releases are created by pushing a semantic-version tag such as `v0.1.0`:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The tag starts the [GoReleaser workflow](./.github/workflows/release.yml),
which builds the CLI for Linux, macOS, and Windows on amd64, arm64, and
arm where supported, then publishes the archives and checksums to a GitHub
Release. The Go package is installed from:

```sh
go install github.com/tongbpm/github-app-token/cmd/github-app-token@v0.1.0
```

[Dependabot](./.github/dependabot.yml) checks Go modules and GitHub Actions
weekly and opens update pull requests.
