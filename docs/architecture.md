# Architecture

Notas sobre la estructura interna del CLI.

## Commands\n\nEach command lives in `cmd/` and wires its own flags; shared logic goes in `internal/registry`.
