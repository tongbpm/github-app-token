# Contributing

## Development

Use the Go version declared in `go.mod`, then run:

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

Keep the package focused on GitHub App installation-token creation and
lifecycle management. Avoid adding general GitHub API clients or persistent
token storage.

## Pull requests

Explain the behavior change, include tests for public behavior, and avoid
including credentials or generated release artifacts. Use semantic commit
messages where practical.
