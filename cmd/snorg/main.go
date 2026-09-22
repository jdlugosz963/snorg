// Command snorg is the supernote-organizer CLI. It is a thin front-end over the
// public snorg package (github.com/jdlugosz963/snorg/pkg/snorg): the commands parse
// flags, source PAGEIDs from arguments or stdin, and format results, delegating every
// capability to a snorg.Client.
//
// The archive path is the global -a/--archive flag, optional when the XDG user config
// ($XDG_CONFIG_HOME/snorg/config.yaml) sets `archive:` (the flag wins). The merged
// config (XDG user config, overridden by -c files) is loaded once in the root Before
// hook via snorg.Resolve and shared by every command:
//
//	snorg [-a <archive-path>] [-c config.yaml ...] [--no-user-config] [-v] <command> [command flags] [args]
//
//	snorg [-a <archive-path>] ingest <file-or-dir>
//	snorg [-a <archive-path>] list [-l] | list keywords|tags [-l]
//	snorg [-a <archive-path>] query <expr>
//	snorg [-a <archive-path>] tag [-r] <tag> [PAGEID ...] | tag -n [-r] <tag> [FILE_ID ...]
//	snorg [-a <archive-path>] retrieve [PAGEID ...]
//	snorg [-a <archive-path>] analyze [--force] [PAGEID ...]
//	snorg [-a <archive-path>] analyze-edit <PAGEID>
//	snorg [-a <archive-path>] export [PAGEID ...]
//	snorg [-a <archive-path>] serve [-l ADDR] [--flat] [PAGEID ...]
//	snorg [-a <archive-path>] migrate [PAGEID ...]
//
// The batch commands (ingest, analyze, migrate) report progress on stderr, so
// stdout stays the machine-readable channel: an animated bar when stderr is a
// terminal, plain per-item lines when it is not, and -v/--verbose replaces either
// with a detailed per-item account of what actually changed. query, list, retrieve
// and export write only their data, on stdout, and are untouched by -v.
//
// retrieve, analyze and export take PAGEIDs as arguments or stdin lines, so
// query pipes into any of them. query itself also reads PAGEIDs from stdin when
// they are piped in, restricting its expression to that set (query A | query B ==
// A∩B); NOT inverts an expression, so query A | query 'NOT B' == A minus B. The
// expression itself composes terms with AND/OR/NOT and parentheses, so most of what
// used to need a pipeline is now one call (see snorg.QuerySyntax).
// analyze-edit takes exactly one PAGEID (it opens $VISUAL/$EDITOR on the page's
// transcription — content plus the title/link names — so it needs the terminal,
// not a pipe).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"

	snorg "github.com/jdlugosz963/snorg/pkg/snorg"
)

func main() {
	if err := root().Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "snorg: "+err.Error())
		os.Exit(1)
	}
}

// app is the state shared by every command, built by the root Before hook: a
// snorg.Client for the resolved archive and merged config.
type app struct {
	client  *snorg.Client
	verbose bool
}

// archiveFlag is the global flag naming the archive root. Being on the root, it
// must precede the command. It is not Required: the path may instead come from the
// `archive:` key in the XDG user config (the flag wins when both are set).
func archiveFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "archive",
		Aliases: []string{"a"},
		Usage:   "archive root `PATH` (holds the FILE_ID sub-dirs); optional if the user config sets archive:",
	}
}

// configFlag is the repeatable global config flag; later files override earlier
// ones (see snorg.LoadConfig).
func configFlag() *cli.StringSliceFlag {
	return &cli.StringSliceFlag{
		Name:    "config",
		Aliases: []string{"c"},
		Usage:   "config YAML `FILE` (repeatable; later files override earlier ones)",
	}
}

// noUserConfigFlag opts out of loading the XDG user config.
func noUserConfigFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  "no-user-config",
		Usage: "ignore the XDG user config (~/.config/snorg/config.yaml)",
	}
}

// verboseFlag turns per-item reporting from a progress bar into a detailed account
// of what each item changed. Global, so like -a/-c it precedes the command.
func verboseFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:    "verbose",
		Aliases: []string{"v"},
		Usage:   "describe what changed per item instead of drawing a progress bar",
	}
}

// commands builds the subcommands, closed over the shared app state. The root
// Before hook populates the app (the client) before any command action runs, so the
// actions can rely on a.client being set.
func commands(a *app) []*cli.Command {
	return []*cli.Command{
		ingestCmd(a),
		listCmd(a),
		retrieveCmd(a),
		queryCmd(a),
		tagCmd(a),
		analyzeCmd(a),
		analyzeEditCmd(a),
		exportCmd(a),
		serveCmd(a),
		migrateCmd(a),
	}
}

const commandNames = "ingest, list, retrieve, query, tag, analyze, analyze-edit, export, serve, migrate"

// root registers the global flags and subcommands and builds the shared client once
// in its Before hook (which urfave/cli runs before the matched subcommand's action).
// The archive path and merged config are resolved by snorg.Resolve, mirroring the
// -a/-c flags. Since -a is not Required, natural subcommand dispatch still routes the
// first positional as the command name, as urfave expects.
func root() *cli.Command {
	a := &app{}
	return &cli.Command{
		Name:                  "snorg",
		Usage:                 "supernote-organizer: ingest .note files into a plaintext archive",
		UsageText:             "snorg [-a <archive-path>] [-c config.yaml ...] [--no-user-config] [-v] <command> [command flags] [args]",
		Flags:                 []cli.Flag{archiveFlag(), configFlag(), noUserConfigFlag(), verboseFlag()},
		Commands:              commands(a),
		EnableShellCompletion: true,
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			client, err := snorg.Resolve(snorg.ResolveOptions{
				ArchivePath:  cmd.String("archive"),
				ConfigFiles:  cmd.StringSlice("config"),
				NoUserConfig: cmd.Bool("no-user-config"),
			})
			if err != nil {
				return ctx, err
			}
			a.client = client
			a.verbose = cmd.Bool("verbose")
			return ctx, nil
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			// Reached only with -a given but no (or an unknown) command.
			return fmt.Errorf("usage: snorg [-a <archive-path>] <command> [args]\n  commands: %s", commandNames)
		},
	}
}

func ingestCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "ingest",
		Usage:     "register a .note file (or all *.note under a dir) into the archive",
		ArgsUsage: "<file-or-dir>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("usage: snorg [-a <archive-path>] ingest <file-or-dir>")
			}

			inputPath := cmd.Args().Get(0)
			info, err := os.Stat(inputPath)
			if err != nil {
				return err
			}
			var paths []string
			if info.IsDir() {
				paths, err = snorg.NoteFiles(inputPath)
				if err != nil {
					return err
				}
				if len(paths) == 0 {
					return fmt.Errorf("no .note files under %s", inputPath)
				}
			} else {
				paths = []string{inputPath}
			}

			// Reporting is per note as it lands, in input order.
			r := newReporter(os.Stderr, len(paths), "ingest", "notes", a.verbose)
			defer r.close()
			var failed int
			results, err := a.client.Ingest(paths, snorg.IngestOptions{
				OnResult: func(res snorg.IngestResult) {
					if res.Err != nil {
						failed++
						r.fail(res.Path, res.Err)
						return
					}
					r.item(res.Path, describeIngest(res), ingestDetail(res))
				},
			})
			if err != nil {
				return err
			}
			r.finish(fmt.Sprintf("ingested %d, failed %d of %d",
				len(results)-failed, failed, len(results)))
			// A page in two of the batch's notes was copied, not moved; snorg keys a
			// page by its device id and cannot represent that, so name it rather than
			// let the two notes take the page from each other on every ingest.
			dups := duplicatePages(results)
			for _, pid := range sorted(slices.Collect(maps.Keys(dups))) {
				fmt.Fprintf(os.Stderr, "warning: page %s is claimed by %v — a copied page cannot keep a unique id\n", pid, dups[pid])
			}
			if n := failed; n > 0 {
				return fmt.Errorf("%d of %d notes failed", n, len(results))
			}
			return nil
		},
	}
}

func listCmd(a *app) *cli.Command {
	longFlag := func() cli.Flag {
		return &cli.BoolFlag{Name: "long", Aliases: []string{"l"}}
	}
	return &cli.Command{
		Name:  "list",
		Usage: "inspect an archive: bare = FILE_IDs; subcommands `keywords`/`tags` enumerate labels",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "long",
				Aliases: []string{"l"},
				Usage:   "annotate each FILE_ID with its note name (source sans .note), tab-separated",
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "keywords",
				Usage: "list the archive's distinct device keywords, one per line; -l adds a tab-separated page count",
				Flags: []cli.Flag{longFlag()},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 0 {
						return fmt.Errorf("usage: snorg [-a <archive-path>] list keywords [-l]")
					}
					vals, err := a.client.Keywords()
					if err != nil {
						return err
					}
					return printValueCounts(vals, cmd.Bool("long"))
				},
			},
			{
				Name:  "tags",
				Usage: "list the archive's distinct snorg tags, one per line; -l adds a tab-separated page count",
				Flags: []cli.Flag{longFlag()},
				Action: func(_ context.Context, cmd *cli.Command) error {
					if cmd.Args().Len() != 0 {
						return fmt.Errorf("usage: snorg [-a <archive-path>] list tags [-l]")
					}
					vals, err := a.client.Tags()
					if err != nil {
						return err
					}
					return printValueCounts(vals, cmd.Bool("long"))
				},
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 0 {
				return fmt.Errorf("usage: snorg [-a <archive-path>] list [-l] | list keywords|tags [-l]")
			}
			ids, err := a.client.List()
			if err != nil {
				return err
			}
			if !cmd.Bool("long") {
				for _, id := range ids {
					fmt.Println(id)
				}
				return nil
			}
			for _, id := range ids {
				nd, err := a.client.ReadNote(id)
				if err != nil {
					return fmt.Errorf("note %s: %w", id, err)
				}
				name := strings.TrimSuffix(nd.Source, ".note")
				if name == "" {
					name = id
				}
				fmt.Printf("%s\t%s\n", id, name)
			}
			return nil
		},
	}
}

// printValueCounts prints one label per line, or "<value>\t<count>" with long set.
func printValueCounts(vals []snorg.ValueCount, long bool) error {
	for _, v := range vals {
		if long {
			fmt.Printf("%s\t%d\n", v.Value, v.Count)
		} else {
			fmt.Println(v.Value)
		}
	}
	return nil
}

func retrieveCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "retrieve",
		Usage:     "print the assembled pages as a JSON object {archive, notes} (no PAGEIDs = read them from stdin)",
		ArgsUsage: "[PAGEID ...]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			pageIDs, err := pageIDArgs(cmd)
			if err != nil {
				return err
			}
			res, err := a.client.Retrieve(pageIDs)
			if err != nil {
				return err
			}
			b, err := json.MarshalIndent(res, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		},
	}
}

// pageIDArgs returns the command's PAGEIDs: the positional arguments, or the
// stdin lines (piped from query) when there are none. Empty is an error.
func pageIDArgs(cmd *cli.Command) ([]string, error) {
	pageIDs := cmd.Args().Slice()
	if len(pageIDs) == 0 {
		var err error
		if pageIDs, err = readLines(os.Stdin); err != nil {
			return nil, err
		}
		if len(pageIDs) == 0 {
			return nil, fmt.Errorf("no PAGEIDs given (arguments or stdin lines)")
		}
	}
	return pageIDs, nil
}

func queryCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:        "query",
		Usage:       "print PAGEIDs of matching pages, one per line (pipe into retrieve/analyze/export); -l/--long annotates them (browse-only, not pipe-safe)",
		ArgsUsage:   "<expr>   e.g. 'starred AND (tag:work OR date:today)'",
		Description: "The filter is a boolean expression, " + snorg.QuerySyntax,
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "long",
				Aliases: []string{"l"},
				Usage:   "annotate each PAGEID with note, page#, *, headings, #keywords and @tags in tab-separated columns (do not pipe downstream)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			// The whole expression is one argument, but the shell splits an
			// unquoted one into words — joining them back means both
			// `query starred AND tag:work` and `query 'a AND (b OR c)'` work.
			expr := strings.Join(cmd.Args().Slice(), " ")
			if strings.TrimSpace(expr) == "" {
				return fmt.Errorf("usage: snorg [-a <archive-path>] query <expr>\n%s", snorg.QuerySyntax)
			}
			pred, err := snorg.ParseQuery(expr)
			if err != nil {
				return err
			}
			// When PAGEIDs are piped in, restrict the filter to that set so
			// successive queries intersect (query A | query B == A∩B).
			if stdinPiped() {
				ids, err := readLines(os.Stdin)
				if err != nil {
					return err
				}
				pred = snorg.MatchAnd(snorg.MatchIDs(ids), pred)
			}
			matches, err := a.client.Query(pred)
			if err != nil {
				return err
			}
			if cmd.Bool("long") {
				return printQueryLong(a.client, matches)
			}
			for _, m := range matches {
				fmt.Println(m.PageID)
			}
			return nil
		},
	}
}

// printQueryLong emits the human-readable, browse-only form of a query result:
// tab-separated columns "<PAGEID>\t<note>\tp<page#>\t<*?>\t<headings ' / '>\t#kw #kw\t@tag @tag",
// where note is the source filename sans ".note", page# comes from note.json's
// placement, * marks a starred page (empty otherwise), headings are the analyzed
// title names (empty until analyze runs), keywords (device metadata, present
// without analysis) are rendered as #kw and snorg-managed tags as @tag — the page's
// effective set, its own plus the ones inherited from its note — so a fuzzy finder
// can filter on either.
// PAGEID stays the first whitespace field on purpose (`awk '{print $1}'` extracts
// it for the fzf → serve workflow). Fixed \t separators keep the columns machine-
// splittable (cut -f) regardless of value widths. This is deliberately NOT the
// bare-PAGEID pipe contract, so it is never fed downstream.
func printQueryLong(c *snorg.Client, matches []snorg.Match) error {
	notes := make(map[string]*snorg.NoteDoc)
	for _, m := range matches {
		nd, ok := notes[m.FileID]
		if !ok {
			read, err := c.ReadNote(m.FileID)
			if err != nil {
				return fmt.Errorf("note %s: %w", m.FileID, err)
			}
			nd = &read
			notes[m.FileID] = nd
		}
		name := strings.TrimSuffix(nd.Source, ".note")
		if name == "" {
			name = m.FileID
		}
		number := 0
		for _, ref := range nd.Pages {
			if ref.ID == m.PageID {
				number = ref.Number
				break
			}
		}
		pd, err := c.ReadPage(m.FileID, m.PageID)
		if err != nil {
			return fmt.Errorf("note %s page %s: %w", m.FileID, m.PageID, err)
		}
		var headings, keywords, tags []string
		for _, t := range pd.Titles {
			if t.Analysis != nil {
				headings = append(headings, t.Analysis.Name)
			}
		}
		for _, k := range pd.Keywords {
			keywords = append(keywords, "#"+k.Text)
		}
		for _, t := range snorg.EffectiveTags(*nd, pd) {
			tags = append(tags, "@"+t)
		}
		star := ""
		if pd.Starred {
			star = "*"
		}
		fmt.Printf("%s\t%s\tp%d\t%s\t%s\t%s\t%s\n",
			m.PageID, name, number, star,
			strings.Join(headings, " / "), strings.Join(keywords, " "), strings.Join(tags, " "))
	}
	return nil
}

func tagCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "tag",
		Usage:     "add (or -r remove) a snorg-managed tag on pages, or with -n on whole notes (no ids = read them from stdin)",
		ArgsUsage: "<tag> [PAGEID ...] (with -n: <tag> [FILE_ID ...])",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "remove", Aliases: []string{"r"}, Usage: "remove the tag instead of adding it"},
			&cli.BoolFlag{Name: "note", Aliases: []string{"n"}, Usage: "tag whole notes: the ids are FILE_IDs and the tag lives in note.json, inherited by every page of the note"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			notes := cmd.Bool("note")
			// -n swaps the id kind wholesale: FILE_IDs (pipe from `list`) instead of
			// PAGEIDs (pipe from `query`), so the two modes share one arg shape.
			unit, idKind := "pages", "PAGEID"
			if notes {
				unit, idKind = "notes", "FILE_ID"
			}
			if len(args) == 0 {
				return fmt.Errorf("usage: snorg [-a <archive-path>] tag [-n] [-r] <tag> [%s ...]", idKind)
			}
			tag, ids := args[0], args[1:]
			if len(ids) == 0 {
				var err error
				if ids, err = readLines(os.Stdin); err != nil {
					return err
				}
				if len(ids) == 0 {
					return fmt.Errorf("no %ss given (arguments or stdin lines)", idKind)
				}
			}
			remove := cmd.Bool("remove")
			tagFn := a.client.Tag
			if notes {
				tagFn = a.client.TagNote
			}
			changed, err := tagFn(tag, ids, remove)
			if err != nil {
				return err
			}
			verb := "tagged"
			if remove {
				verb = "untagged"
			}
			fmt.Printf("%s %d of %d %s\n", verb, changed, len(ids), unit)
			return nil
		},
	}
}

func analyzeCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "analyze",
		Usage:     "vision-LLM analysis of pages; unchanged pages are skipped (no PAGEIDs = read them from stdin)",
		ArgsUsage: "[PAGEID ...]",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "force", Usage: "re-analyze even when the page is unchanged"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			pageIDs, err := pageIDArgs(cmd)
			if err != nil {
				return err
			}
			// Built once, before any page: bad credentials fail here rather than
			// after the first LLM call.
			prov, err := a.client.NewProvider()
			if err != nil {
				return err
			}
			// One failure never aborts the batch — analysis of the rest is still
			// worth the wait. Reporting is per page as it lands, so a long batch
			// shows progress instead of going quiet until the end.
			r := newReporter(os.Stderr, len(pageIDs), "analyze", "pages", a.verbose)
			defer r.close()
			failed, skipped, analyzed, conflicted := 0, 0, 0, 0
			_, err = a.client.Analyze(ctx, prov, pageIDs, snorg.AnalyzeOptions{
				Force: cmd.Bool("force"),
				OnResult: func(res snorg.AnalyzeResult) {
					if res.Err != nil {
						failed++
						r.fail(res.PageID, res.Err)
						return
					}
					switch {
					case res.Skipped:
						skipped++
					default:
						analyzed++
					}
					if conflictedResult(res) {
						conflicted++
					}
					r.item(res.PageID, describeAnalyze(res), analyzeDetail(res))
				},
			})
			if err != nil {
				return err
			}
			r.finish(fmt.Sprintf("analyzed %d, skipped %d, conflicted %d, failed %d of %d page(s)",
				analyzed, skipped, conflicted, failed, len(pageIDs)))
			if conflicted > 0 {
				fmt.Fprintf(os.Stderr, "conflict markers written; resolve with: snorg -a %s analyze-edit <PAGEID>\n", a.client.ArchivePath())
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d pages failed", failed, len(pageIDs))
			}
			return nil
		},
	}
}

func analyzeEditCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "analyze-edit",
		Usage:     "edit a page's transcription and title/link names in $VISUAL/$EDITOR (edits survive re-analysis)",
		ArgsUsage: "<PAGEID>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			// Exactly one positional PAGEID, no stdin fallback: the editor
			// needs the terminal, not a pipe. Needs no provider config —
			// pages can be transcribed by hand without any LLM involved.
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("usage: snorg [-a <archive-path>] analyze-edit <PAGEID>")
			}
			editor, err := snorg.EditorFromEnv()
			if err != nil {
				return err
			}
			pageID := cmd.Args().Get(0)
			res, err := a.client.EditPage(pageID, editor)
			if err != nil {
				return err
			}
			// One page, no batch: this is a result, not progress, so it stays on
			// stdout like tag's line does.
			fmt.Printf("%s: %s\n", pageID, describeEdit(res))
			if a.verbose {
				for _, d := range editDetail(res) {
					fmt.Printf("  %s\n", d)
				}
			}
			return nil
		},
	}
}

// readLines reads whitespace-trimmed non-empty lines (PAGEIDs piped from query).
func readLines(r *os.File) ([]string, error) {
	var out []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			out = append(out, line)
		}
	}
	return out, sc.Err()
}

func exportCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "export",
		Usage:     "render the retrieved pages through the config's pongo2 template (no PAGEIDs = read them from stdin)",
		ArgsUsage: "[PAGEID ...]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			pageIDs, err := pageIDArgs(cmd)
			if err != nil {
				return err
			}
			out, err := a.client.Export(pageIDs)
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		},
	}
}

func serveCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "serve",
		Usage:     "serve a local HTML viewer for the pages (no PAGEIDs and no pipe = the whole archive)",
		ArgsUsage: "[PAGEID ...]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "listen",
				Aliases: []string{"l"},
				Usage:   "`ADDR` to listen on",
				Value:   "127.0.0.1:8080",
			},
			&cli.BoolFlag{
				Name:    "flat",
				Aliases: []string{"f"},
				Usage:   "one flat page gallery instead of grouping by note",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			pageIDs, err := servePageIDs(a, cmd)
			if err != nil {
				return err
			}
			res, err := a.client.Retrieve(pageIDs)
			if err != nil {
				return err
			}
			handler, err := a.client.ServeHandler(pageIDs, cmd.Bool("flat"))
			if err != nil {
				return err
			}
			addr := cmd.String("listen")
			pages := 0
			for _, n := range res.Notes {
				pages += len(n.Pages)
			}
			fmt.Fprintf(os.Stderr, "snorg: serving %d page(s) across %d note(s) on http://%s/\n", pages, len(res.Notes), addr)
			return http.ListenAndServe(addr, handler)
		},
	}
}

// servePageIDs is serve's PAGEID selection: positional arguments, or piped stdin
// lines, or — with neither (a terminal stdin) — every page in the archive, so a
// bare `serve` opens the whole archive.
func servePageIDs(a *app, cmd *cli.Command) ([]string, error) {
	if cmd.Args().Len() > 0 {
		return cmd.Args().Slice(), nil
	}
	if stdinPiped() {
		return readLines(os.Stdin)
	}
	matches, err := a.client.Query(snorg.MatchAll)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m.PageID
	}
	return ids, nil
}

func migrateCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "migrate",
		Usage:     "upgrade note.json/page JSON to the current schema version (no PAGEIDs and no pipe = whole archive)",
		ArgsUsage: "[PAGEID ...]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			// Selection mirrors serve, but routes to the archive's un-gated
			// migrator (not query): migrate must read the stale grammars it exists
			// to repair, which the gated readers refuse.
			// The result count is not knowable up front (a page can yield both a
			// JSON result and a .md.diff one), so the reporter runs its counter
			// form rather than a bar that could pass 100%.
			r := newReporter(os.Stderr, 0, "migrate", "files", a.verbose)
			defer r.close()
			migrated, current, failed := 0, 0, 0
			opts := snorg.MigrateOptions{OnResult: func(res snorg.MigrateResult) {
				id := res.Kind + " " + res.ID
				switch {
				case res.Err != nil:
					failed++
					r.fail(id, res.Err)
				case res.Outcome == snorg.MigrateUpgraded:
					migrated++
					r.item(id, string(res.Outcome), nil)
				default:
					// An already-current file is progress but not news: it moves
					// the counter, and only -v spends a line on it.
					current++
					if a.verbose {
						r.item(id, string(res.Outcome), nil)
					} else {
						r.tick(id)
					}
				}
			}}

			var results []snorg.MigrateResult
			var err error
			switch {
			case cmd.Args().Len() > 0:
				results, err = a.client.Migrate(cmd.Args().Slice(), opts)
			case stdinPiped():
				var ids []string
				if ids, err = readLines(os.Stdin); err == nil {
					results, err = a.client.Migrate(ids, opts)
				}
			default:
				results, err = a.client.MigrateAll(opts)
			}
			if err != nil {
				return err
			}

			r.finish(fmt.Sprintf("migrated %d, current %d, failed %d of %d file(s) -> schema v%d",
				migrated, current, failed, len(results), snorg.CurrentSchemaVersion))
			if failed > 0 {
				return fmt.Errorf("%d of %d file(s) failed to migrate", failed, len(results))
			}
			return nil
		},
	}
}

// conflictedResult reports whether an analyzed page left conflict markers anywhere:
// in its whole-page content, or in any one of a templated page's boxes.
func conflictedResult(r snorg.AnalyzeResult) bool {
	if r.Content != nil && r.Content.Conflicts {
		return true
	}
	for _, rr := range r.Regions {
		if rr.Text.Conflicts {
			return true
		}
	}
	return false
}

// stdinPiped reports whether stdin is a pipe or redirect (not a terminal), i.e.
// PAGEIDs are being piped in. Reading a terminal stdin would block.
func stdinPiped() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice == 0
}
