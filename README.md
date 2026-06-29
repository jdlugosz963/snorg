# snorg

Supernote organizer: ingests Supernote `.note` files into a plaintext,
machine-readable, VCS-friendly archive for later retrieval and export.

The `.note` binary format is handled by shelling out to
[`supernote-tool`](https://github.com/jya-dev/supernote-tool) (must be on `PATH`).
Go, stdlib only.

## Usage

```
go run ./cmd/snorg ingest <file.note> <archive>   # register a note
go run ./cmd/snorg list <archive>                 # list FILE_IDs
go run ./cmd/snorg retrieve <archive> <FILE_ID>   # assembled note as JSON
```

Archive layout: `<archive>/<FILE_ID>/{note.json,<PAGEID>.json,<PAGEID>.svg}`.

See `docs/` for principles, architecture, the `.note` format, and the read contract.
