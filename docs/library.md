# Library API (`pkg/snorg`)

snorg's capabilities are usable from Go, not only the CLI. Import the public
package; the `internal/*` packages are not importable from outside the module, so
this package is the whole supported surface.

```go
import snorg "github.com/jdlugosz963/snorg/pkg/snorg"
```

## Client

A `Client` bundles an archive root with merged configuration.

- `Open(archivePath string, cfg *Config) (*Client, error)` — explicit root; `cfg`
  may be `nil` for built-in defaults. Build `cfg` with `LoadConfig(paths)`.
- `Resolve(ResolveOptions) (*Client, error)` — the CLI's resolution: archive path
  from the option or the config's `archive:` key (`~` expanded), config layered
  XDG user → `<archive>/config.yaml` → `-c` files (later wins).

`Client.ArchivePath()` / `Client.Config()` expose the resolved root and config.

## Capabilities

| Method | Does |
|---|---|
| `List()` | archived FILE_IDs |
| `Query(pred)` | pages matching a `Predicate` |
| `ParseFilter(word, args)` | build a `Predicate` from the CLI filter DSL (`all`, `starred`, `note`, `keyword`, `content`, `date`, `not …`) |
| `Retrieve(pageIDs)` | assemble pages into a `*Result` (`{Archive, Notes}`) |
| `ReadNote/ReadPage/ReadSVG/FindPage` | raw on-disk document access |
| `Ingest(paths, jobs)` | register `.note` files (`NoteFiles(dir)` enumerates them) |
| `Export(pageIDs)` | render through the config's template (`RenderTemplate` for an arbitrary one) |
| `ServeHandler(pageIDs, flat)` | the built-in viewer as an `http.Handler` (empty = whole archive) |
| `Analyze(ctx, pageIDs, opts)` | vision-LLM transcription (config-driven provider + prompts) |
| `AnalyzePage(ctx, prov, spec, id, force)` | one page with a caller-supplied `Provider` |
| `Migrate(pageIDs)` / `MigrateAll()` | schema upgrade |
| `PageBuffer(id)` / `ApplyPage(id, buf)` | programmatic transcription edit — no `$EDITOR` |

The predicate constructors (`All`, `Starred`, `Unanalyzed`, `Not`, `And`, `InSet`,
`InNote`, `Keyword`, `Date`, plus `Client.Content`) compose filters directly.

Editing: `PageBuffer`/`ApplyPage` are the library edit path (round-trip the buffer
yourself). `EditPage(id, editor)` and `EditorFromEnv()` are the interactive
convenience the CLI's `analyze-edit` uses — they spawn `$EDITOR`, so a library that
supplies its own UI should use `PageBuffer`/`ApplyPage` instead.

## Example

```go
c, err := snorg.Open("/path/to/archive", nil)
if err != nil { log.Fatal(err) }

pred, _ := c.ParseFilter("date", []string{"today"})
matches, _ := c.Query(pred)

ids := make([]string, len(matches))
for i, m := range matches { ids[i] = m.PageID }

res, _ := c.Retrieve(ids) // *snorg.Result
for _, n := range res.Notes {
    for _, p := range n.Pages {
        fmt.Println(p.PageID, p.Analysis.Content)
    }
}
```

All types a method returns are re-exported from this package as aliases
(`snorg.Result`, `snorg.NoteView`, `snorg.Match`, `snorg.Spec`, `snorg.Config`, …),
so importing `pkg/snorg` alone is sufficient — no `internal/*` import is ever needed.
