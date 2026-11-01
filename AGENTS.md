# Project Guidelines

## Scope

Keep this project focused on creating and caching GitHub App installation
access tokens. Do not expand it into a general GitHub API client or add
persistent token storage.

The root package is the public Go library. The CLI entry point is
`cmd/github-app-token`.

## Security

Never commit, log, print, or persist GitHub App private keys or installation
tokens. Treat changes to credential parsing, JWT creation, HTTP requests, and
token caching as security-sensitive. See [SECURITY.md](./SECURITY.md).

## Validation

Use Go 1.23 or newer. Before submitting changes, run:

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./...
go mod verify
```

Changes to authentication or caching should include focused tests. Keep
package examples and public API documentation current.

## Releases

Do not create a release tag unless explicitly requested. Releases use
semantic-version tags and GoReleaser; see [README.md](./README.md).
