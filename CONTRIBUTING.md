# Contributing

Thanks for helping! WalletFlow values being simple to use and simple to build on.

## Development

```bash
go run ./cmd/walletflow -demo     # sample data, no keys
go test ./...                     # unit tests; chain clients run on JSON fixtures
gofmt -l . && go vet ./...
```

Live checks against real APIs are opt-in: `WF_LIVE=1 go test -run Live ./internal/chain/...`.

## Guidelines

- Keep the dependency list short. The standard library first.
- Amounts are integers in minimal units (`amount_raw` + decimals). Never floats for money.
- New UI text goes through `{{t "..."}}` in templates, `t('...')` in scripts and `.T(...)` in Go,
  with a Russian entry in `internal/i18n/ru.go`. `go test ./internal/i18n` fails on a missing one.
- One logical change per pull request, with a test where it makes sense.
- Never ask for or store seed phrases or private keys.

## Adding a network

A chain client lives in `internal/chain/<name>` and implements `Pull` (download new transfers page by page,
saving cursors) and `PerDay` (activity estimate from the newest page). Register the chain in
`internal/chain/chain.go` and wire it in `internal/syncer`.
