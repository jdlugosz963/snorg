// Command snorg is the supernote-organizer CLI. The archive path is a required
// global flag (-a/--archive); the merged config (archive config.yaml, overridden
// by -c files) is loaded once in the root Before hook and shared by every command:
//
//	snorg -a <archive-path> [-c config.yaml ...] [--no-archive-config] <command> [command flags] [args]
//
//	snorg -a <archive-path> ingest [-j N] <file-or-dir>
//	snorg -a <archive-path> list
//	snorg -a <archive-path> query <filter> [arg]
//	snorg -a <archive-path> retrieve [PAGEID ...]
//	snorg -a <archive-path> analyze [--force] [PAGEID ...]
//	snorg -a <archive-path> analyze-edit <PAGEID>
//	snorg -a <archive-path> export [PAGEID ...]
//	snorg -a <archive-path> serve [-l ADDR] [PAGEID ...]
//
// retrieve, analyze and export take PAGEIDs as arguments or stdin lines, so
// query pipes into any of them. query itself also reads PAGEIDs from stdin when
// they are piped in, restricting its filter to that set (query A | query B == A∩B).
// A "not" prefix inverts any filter (query not starred == the non-starred pages),
// so query A | query not B == A minus B.
// analyze-edit takes exactly one PAGEID (it opens $VISUAL/$EDITOR on the page's
// transcription — content plus the title/link names — so it needs the terminal,
// not a pipe).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/jdlugosz963/snorg/internal/analyze"
	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/config"
	"github.com/jdlugosz963/snorg/internal/edit"
	"github.com/jdlugosz963/snorg/internal/export"
	"github.com/jdlugosz963/snorg/internal/ingest"
	"github.com/jdlugosz963/snorg/internal/query"
	"github.com/jdlugosz963/snorg/internal/retrieve"
	"github.com/jdlugosz963/snorg/internal/serve"
	"github.com/jdlugosz963/snorg/internal/snote/sntool"
)

func main() {
	if err := root().Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "snorg: "+err.Error())
		os.Exit(1)
	}
}

// app is the state shared by every command, built by the root Before hook: the
// archive and the merged config, loaded once. Commands pick the config sections
// they need and validate only those.
type app struct {
	path string // archive path from the -a/--archive flag
	arch *archive.Archive
	cfg  *config.Config
}

// archiveFlag is the required global flag naming the archive root. Being on the
// root, it must precede the command (urfave enforces Required for every command
// except --help/completion, which are exempt).
func archiveFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:     "archive",
		Aliases:  []string{"a"},
		Usage:    "archive root `PATH` (holds the FILE_ID sub-dirs and config.yaml)",
		Required: true,
	}
}

// archiveConfigName is the per-archive default config, loaded from the archive
// root and merged under any -c files (which override it).
const archiveConfigName = "config.yaml"

// configFlag is the repeatable global config flag; later files override earlier
// ones (see config.Load).
func configFlag() *cli.StringSliceFlag {
	return &cli.StringSliceFlag{
		Name:    "config",
		Aliases: []string{"c"},
		Usage:   "config YAML `FILE` (repeatable; later files override earlier ones)",
	}
}

// noArchiveConfigFlag opts out of loading <archive-path>/config.yaml.
func noArchiveConfigFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  "no-archive-config",
		Usage: "ignore <archive-path>/config.yaml (use only -c files)",
	}
}

// configPaths orders the archive config (if present and not disabled) before the
// -c files, so -c overrides the archive config via config.Load's later-wins merge.
func configPaths(archivePath string, cliPaths []string, noArchive bool) []string {
	var paths []string
	if !noArchive {
		p := filepath.Join(archivePath, archiveConfigName)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			paths = append(paths, p)
		}
	}
	return append(paths, cliPaths...)
}

// commands builds the subcommands, closed over the shared app state. The root
// Before hook populates the app (config + archive) before any command action
// runs, so the actions can rely on a.cfg being set.
func commands(a *app) []*cli.Command {
	return []*cli.Command{
		ingestCmd(a),
		listCmd(a),
		retrieveCmd(a),
		queryCmd(a),
		analyzeCmd(a),
		analyzeEditCmd(a),
		exportCmd(a),
		serveCmd(a),
	}
}

const commandNames = "ingest, list, retrieve, query, analyze, analyze-edit, export, serve"

// root registers the global flags and subcommands and loads the merged config
// once in its Before hook, which urfave/cli runs as part of the command chain
// before the matched subcommand's action. Natural subcommand dispatch does the
// rest: the archive path is a required global flag (-a), so the first positional
// argument is the command name, as urfave expects.
func root() *cli.Command {
	a := &app{}
	return &cli.Command{
		Name:                  "snorg",
		Usage:                 "supernote-organizer: ingest .note files into a plaintext archive",
		UsageText:             "snorg -a <archive-path> [-c config.yaml ...] [--no-archive-config] <command> [command flags] [args]",
		Flags:                 []cli.Flag{archiveFlag(), configFlag(), noArchiveConfigFlag()},
		Commands:              commands(a),
		EnableShellCompletion: true,
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			archivePath := cmd.String("archive")
			cfg, err := config.Load(configPaths(archivePath, cmd.StringSlice("config"), cmd.Bool("no-archive-config")))
			if err != nil {
				return ctx, err
			}
			a.path, a.arch, a.cfg = archivePath, archive.New(archivePath), cfg
			return ctx, nil
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			// Reached only with -a given but no (or an unknown) command.
			return fmt.Errorf("usage: snorg -a <archive-path> <command> [args]\n  commands: %s", commandNames)
		},
	}
}

func ingestCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "ingest",
		Usage:     "register a .note file (or all *.note under a dir) into the archive",
		ArgsUsage: "<file-or-dir>",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "jobs", Aliases: []string{"j"}, Usage: "max concurrent notes (0 = number of CPUs)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("usage: snorg -a <archive-path> ingest [-j N] <file-or-dir>")
			}
			if err := a.cfg.ValidateIngest(); err != nil {
				return err
			}

			inputPath := cmd.Args().Get(0)
			info, err := os.Stat(inputPath)
			if err != nil {
				return err
			}
			var paths []string
			if info.IsDir() {
				paths, err = ingest.NoteFiles(inputPath)
				if err != nil {
					return err
				}
				if len(paths) == 0 {
					return fmt.Errorf("no .note files under %s", inputPath)
				}
			} else {
				paths = []string{inputPath}
			}

			s := a.cfg.Ingest.SVG
			a.arch.SVG = archive.SVGPipeline{
				Links:      *s.Links,
				Navigation: *s.Navigation,
				Format:     *s.Format,
				Background: archive.BackgroundMode(s.Background),
				Colors:     s.Colors,
			}
			results := ingest.RunMany(sntool.New(), a.arch, paths, cmd.Int("jobs"))
			failed := 0
			for _, r := range results {
				if r.Err != nil {
					failed++
					fmt.Fprintf(os.Stderr, "failed %s: %v\n", r.Path, r.Err)
					continue
				}
				fmt.Printf("ingested %s (%d pages) -> %s/%s\n", r.Note.Source, len(r.Note.Pages), a.path, r.Note.FileID)
			}
			if len(paths) > 1 || failed > 0 {
				fmt.Printf("ingested %d, failed %d of %d\n", len(results)-failed, failed, len(results))
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d notes failed", failed, len(results))
			}
			return nil
		},
	}
}

func listCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "list archived FILE_IDs, one per line",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 0 {
				return fmt.Errorf("usage: snorg -a <archive-path> list")
			}
			ids, err := retrieve.List(a.arch)
			if err != nil {
				return err
			}
			for _, id := range ids {
				fmt.Println(id)
			}
			return nil
		},
	}
}

func retrieveCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "retrieve",
		Usage:     "print the assembled pages as a JSON array of notes (no PAGEIDs = read them from stdin)",
		ArgsUsage: "[PAGEID ...]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			pageIDs, err := pageIDArgs(cmd)
			if err != nil {
				return err
			}
			views, err := retrieve.Get(a.arch, pageIDs)
			if err != nil {
				return err
			}
			b, err := json.MarshalIndent(views, "", "  ")
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

const queryFilters = "all, note <FILE_ID>, unanalyzed, keyword <regexp>, content <regexp>, starred, date <spec>, not <filter> (inverse)"

func queryCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "query",
		Usage:     "print PAGEIDs of matching pages, one per line (pipe into retrieve/analyze/export)",
		ArgsUsage: "<filter> [arg]   (filters: " + queryFilters + ")",
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 1 {
				return fmt.Errorf("usage: snorg -a <archive-path> query <filter> [arg]\n  filters: %s", queryFilters)
			}
			pred, err := queryPredicate(a.arch, args[0], args[1:])
			if err != nil {
				return err
			}
			// Piped PAGEIDs (query A | query B) restrict the filter to that set,
			// so filters intersect. A terminal stdin is left alone (no blocking).
			if stdinPiped() {
				ids, err := readLines(os.Stdin)
				if err != nil {
					return err
				}
				pred = query.And(query.InSet(ids), pred)
			}
			matches, err := query.Pages(a.arch, pred)
			if err != nil {
				return err
			}
			for _, m := range matches {
				fmt.Println(m.PageID)
			}
			return nil
		},
	}
}

func queryPredicate(a *archive.Archive, filter string, args []string) (query.Predicate, error) {
	arity := func(n int, usage string) error {
		if len(args) != n {
			return fmt.Errorf("usage: snorg -a <archive-path> query %s", usage)
		}
		return nil
	}
	// "not" is a prefix that inverts any filter: it recurses on the rest (so the
	// inner filter's own arg parsing/arity apply verbatim) and negates the result.
	if filter == "not" {
		if len(args) < 1 {
			return nil, fmt.Errorf("usage: snorg -a <archive-path> query not <filter> [arg]\n  filters: %s", queryFilters)
		}
		inner, err := queryPredicate(a, args[0], args[1:])
		if err != nil {
			return nil, err
		}
		return query.Not(inner), nil
	}
	switch filter {
	case "all":
		return query.All, arity(0, "all")
	case "starred":
		return query.Starred, arity(0, "starred")
	case "unanalyzed":
		return query.Unanalyzed, arity(0, "unanalyzed")
	case "note":
		if err := arity(1, "note <FILE_ID>"); err != nil {
			return nil, err
		}
		return query.InNote(args[0]), nil
	case "keyword":
		if err := arity(1, "keyword <regexp>"); err != nil {
			return nil, err
		}
		re, err := regexp.Compile(args[0])
		if err != nil {
			return nil, fmt.Errorf("invalid keyword regexp: %w", err)
		}
		return query.Keyword(re), nil
	case "content":
		if err := arity(1, "content <regexp>"); err != nil {
			return nil, err
		}
		re, err := regexp.Compile(args[0])
		if err != nil {
			return nil, fmt.Errorf("invalid content regexp: %w", err)
		}
		return query.Content(a, re), nil
	case "date":
		if err := arity(1, "date <spec>   (today|yesterday|YYYY-MM-DD|FROM..TO, open ends ok)"); err != nil {
			return nil, err
		}
		from, to, err := parseDateSpec(args[0])
		if err != nil {
			return nil, err
		}
		return query.Date(from, to), nil
	default:
		return nil, fmt.Errorf("unknown filter: %q (want: %s)", filter, queryFilters)
	}
}

// parseDateSpec turns a date filter argument into an inclusive [from, to] range
// formatted "YYYYMMDD" (an empty bound is open). Accepts "today"/"yesterday", a
// single "YYYY-MM-DD" day, and "FROM..TO" ranges with either end omitted.
func parseDateSpec(spec string) (from, to string, err error) {
	day := func(s string) (string, error) {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return "", fmt.Errorf("invalid date %q (want YYYY-MM-DD): %w", s, err)
		}
		return t.Format("20060102"), nil
	}
	switch spec {
	case "today":
		d := time.Now().Format("20060102")
		return d, d, nil
	case "yesterday":
		d := time.Now().AddDate(0, 0, -1).Format("20060102")
		return d, d, nil
	}
	lo, hi, isRange := strings.Cut(spec, "..")
	if !isRange {
		d, err := day(spec)
		return d, d, err
	}
	if lo != "" {
		if from, err = day(lo); err != nil {
			return "", "", err
		}
	}
	if hi != "" {
		if to, err = day(hi); err != nil {
			return "", "", err
		}
	}
	if from == "" && to == "" {
		return "", "", fmt.Errorf("empty date range %q", spec)
	}
	return from, to, nil
}

// stdinPiped reports whether stdin is a pipe or redirect (not a terminal), i.e.
// PAGEIDs are being piped into query. Reading a terminal stdin would block.
func stdinPiped() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice == 0
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
			cfg := a.cfg
			if err := cfg.ResolveAPIKey(); err != nil {
				return err
			}
			if err := cfg.ValidateProvider(); err != nil {
				return err
			}
			pageIDs, err := pageIDArgs(cmd)
			if err != nil {
				return err
			}

			tr, err := analyze.NewOpenAI(cfg.Provider.Endpoint, cfg.Provider.APIKey, cfg.Provider.Model)
			if err != nil {
				return err
			}
			spec := analyze.Spec{
				Content: cfg.Analysis.Content.Prompt,
				Update:  cfg.Analysis.Content.UpdatePrompt,
				Title:   cfg.Analysis.Titles.Prompt,
				Link:    cfg.Analysis.Links.Prompt,
			}
			for name, t := range cfg.Analysis.Fields {
				spec.Fields = append(spec.Fields, analyze.Field{Name: name, Prompt: t.Prompt})
			}

			// Sequential on purpose (LLM rate limits); one failure never aborts
			// the batch — analysis of the rest is still worth the wait.
			failed, conflicted := 0, false
			for _, pageID := range pageIDs {
				outcome, err := analyze.Page(ctx, a.arch, tr, tr, spec, pageID, cmd.Bool("force"))
				if err != nil {
					failed++
					fmt.Fprintf(os.Stderr, "failed %s: %v\n", pageID, err)
					continue
				}
				conflicted = conflicted || outcome == analyze.Conflicted
				fmt.Printf("%s: %s\n", pageID, outcome)
			}
			if conflicted {
				fmt.Fprintf(os.Stderr, "conflict markers written; resolve with: snorg -a %s analyze-edit <PAGEID>\n", a.path)
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
				return fmt.Errorf("usage: snorg -a <archive-path> analyze-edit <PAGEID>")
			}
			editor, err := edit.EditorFromEnv()
			if err != nil {
				return err
			}
			pageID := cmd.Args().Get(0)
			outcome, namesChanged, err := edit.Page(a.arch, pageID, editor)
			if err != nil {
				return err
			}
			if namesChanged > 0 {
				fmt.Printf("%s: %s, %d name(s) updated\n", pageID, outcome, namesChanged)
			} else {
				fmt.Printf("%s: %s\n", pageID, outcome)
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
			if a.cfg.Export.Template == "" {
				return fmt.Errorf("export.template is required")
			}
			pageIDs, err := pageIDArgs(cmd)
			if err != nil {
				return err
			}
			views, err := retrieve.Get(a.arch, pageIDs)
			if err != nil {
				return err
			}
			out, err := export.Render(views, a.cfg.Export.Template)
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
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			pageIDs, err := servePageIDs(a, cmd)
			if err != nil {
				return err
			}
			views, err := retrieve.Get(a.arch, pageIDs)
			if err != nil {
				return err
			}
			addr := cmd.String("listen")
			fmt.Fprintf(os.Stderr, "snorg: serving %d note(s) on http://%s/\n", len(views), addr)
			return http.ListenAndServe(addr, serve.Handler(a.arch, views))
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
	matches, err := query.Pages(a.arch, query.All)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(matches))
	for i, m := range matches {
		ids[i] = m.PageID
	}
	return ids, nil
}
