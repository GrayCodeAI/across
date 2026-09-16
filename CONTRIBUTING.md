# Contributing to Across

Thanks for your interest. Across is in local alpha — contributions should be small, focused, and correct.

## Ground rules

1. **Correctness over features.** A smaller correct change beats a larger speculative one.
2. **Evidence over confidence.** Use `OBSERVED`/`STATED`/`UNKNOWN` honestly. Never claim something is verified without execution evidence.
3. **No silent destructive changes.** Migrations preserve provenance. Deletion uses tombstones.
4. **Prefer unknowns over invention.** If evidence is insufficient, return `unknown`.

## Getting started

```bash
git clone https://github.com/GrayCodeAI/across.git
cd across
go mod tidy
make build
make test
make test-race
```

## Making changes

- One concern per commit. Write a concise commit message explaining *why*.
- Run `make check` (vet + test-race) before opening a PR.
- Add tests for new behavior. A feature without a test is not implemented.
- Update docs in the same commit if behavior changes.

## Code style

- `gofmt -w .` — no exceptions.
- No comments unless asked. Code should explain itself.
- Errors are values. Handle them or return them explicitly.
- Keep the UI thin. Core logic stays in Go, not the web layer.

## Reporting bugs

Open an issue with:
- Across version (`across version`)
- OS and Go version
- What you expected vs. what happened
- The failing command or a minimal reproduction

## Pull requests

- Keep PRs focused. One feature or fix per PR.
- Describe the problem and the approach, not just the diff.
- Be ready to discuss alternatives. The simplest correct solution wins.

## License

By contributing, you agree your contributions will be licensed under the MIT License.
