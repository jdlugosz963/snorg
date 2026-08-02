// Command snorg is the supernote-organizer CLI. The archive path is the global
// -a/--archive flag, optional when the XDG user config
// ($XDG_CONFIG_HOME/snorg/config.yaml) sets `archive:` (the flag wins). The merged
// config (XDG user config, overridden by the archive's config.yaml, overridden by
// -c files) is loaded once in the root Before hook and shared by every command:
//
//	snorg [-a <archive-path>] [-c config.yaml ...] [--no-archive-config] [--no-user-config] <command> [command flags] [args]
//
//	snorg [-a <archive-path>] ingest [-j N] <file-or-dir>
//	snorg [-a <archive-path>] list
//	snorg [-a <archive-path>] query <filter> [arg]
//	snorg [-a <archive-path>] retrieve [PAGEID ...]
//	snorg [-a <archive-path>] analyze [--force] [PAGEID ...]
//	snorg [-a <archive-path>] analyze-edit <PAGEID>
//	snorg [-a <archive-path>] export [PAGEID ...]
//	snorg [-a <archive-path>] serve [-l ADDR] [--flat] [PAGEID ...]
//	snorg [-a <archive-path>] migrate [PAGEID ...]
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

// archiveFlag is the global flag naming the archive root. Being on the root, it
// must precede the command. It is not Required: the path may instead come from the
// `archive:` key in the XDG user config (the flag wins when both are set).
func archiveFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "archive",
		Aliases: []string{"a"},
		Usage:   "archive root `PATH` (holds the FILE_ID sub-dirs and config.yaml); optional if the user config sets archive:",
	}
}

// archiveConfigName is the per-archive default config, loaded from the archive
// root and merged over the XDG user config, under any -c files (which override it).
const archiveConfigName = "config.yaml"

// userConfigPath returns the XDG user config file
// ($XDG_CONFIG_HOME/snorg/config.yaml, i.e. ~/.config/snorg/config.yaml), the
// lowest-precedence config layer and the natural home for a default `archive:`.
// Empty when the user config dir can't be resolved.
func userConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "snorg", archiveConfigName)
}

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
		Usage: "ignore <archive-path>/config.yaml",
	}
}

// noUserConfigFlag opts out of loading the XDG user config.
func noUserConfigFlag() *cli.BoolFlag {
	return &cli.BoolFlag{
		Name:  "no-user-config",
		Usage: "ignore the XDG user config (~/.config/snorg/config.yaml)",
	}
}

// configPaths orders the config layers lowest-precedence first for config.Load's
// later-wins merge: the XDG user config, then the archive's config.yaml, then the
// -c files. A file layer is skipped when absent, a directory, or opted out.
func configPaths(userPath, archivePath string, cliPaths []string, noUser, noArchive bool) []string {
	var paths []string
	isFile := func(p string) bool {
		st, err := os.Stat(p)
		return err == nil && !st.IsDir()
	}
	if !noUser && userPath != "" && isFile(userPath) {
		paths = append(paths, userPath)
	}
	if !noArchive {
		if p := filepath.Join(archivePath, archiveConfigName); isFile(p) {
			paths = append(paths, p)
		}
	}
	return append(paths, cliPaths...)
}

// expandHome expands a leading ~ or ~/ in a path to the user's home directory, so
// a config `archive: ~/notes/sn` resolves like the shell would (YAML/Go do not).
// Returns p unchanged when it has no ~ prefix or the home dir can't be resolved.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
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
		migrateCmd(a),
	}
}

const commandNames = "ingest, list, retrieve, query, analyze, analyze-edit, export, serve, migrate"

// root registers the global flags and subcommands and loads the merged config
// once in its Before hook, which urfave/cli runs as part of the command chain
// before the matched subcommand's action. The archive path comes from the global
// -a flag, or falls back to the `archive:` key in the XDG user config when the flag
// is absent (the flag wins). Since -a is no longer Required, natural subcommand
// dispatch still routes the first positional as the command name, as urfave expects.
func root() *cli.Command {
	a := &app{}
	return &cli.Command{
		Name:                  "snorg",
		Usage:                 "supernote-organizer: ingest .note files into a plaintext archive",
		UsageText:             "snorg [-a <archive-path>] [-c config.yaml ...] [--no-archive-config] [--no-user-config] <command> [command flags] [args]",
		Flags:                 []cli.Flag{archiveFlag(), configFlag(), noArchiveConfigFlag(), noUserConfigFlag()},
		Commands:              commands(a),
		EnableShellCompletion: true,
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			userPath := userConfigPath()
			noUser, noArchive := cmd.Bool("no-user-config"), cmd.Bool("no-archive-config")
			cliPaths := cmd.StringSlice("config")

			// Resolve the archive path before we can locate the archive's own
			// config: the -a flag wins, else the `archive:` key from the layers
			// that don't depend on the archive path (XDG user config + -c files).
			archivePath := cmd.String("archive")
			if archivePath == "" {
				preCfg, err := config.Load(configPaths(userPath, "", cliPaths, noUser, true))
				if err != nil {
					return ctx, err
				}
				archivePath = preCfg.Archive
			}
			if archivePath == "" {
				return ctx, fmt.Errorf("no archive path: pass -a/--archive or set archive: in %s", userPath)
			}
			archivePath = expandHome(archivePath)

			cfg, err := config.Load(configPaths(userPath, archivePath, cliPaths, noUser, noArchive))
			if err != nil {
				return ctx, err
			}
			a.path, a.arch, a.cfg = archivePath, archive.New(archivePath), cfg
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
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "jobs", Aliases: []string{"j"}, Usage: "max concurrent notes (0 = number of CPUs)"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("usage: snorg [-a <archive-path>] ingest [-j N] <file-or-dir>")
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
		Usage: "list archived FILE_IDs, one per line; -l/--long adds the note name (tab-separated)",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "long",
				Aliases: []string{"l"},
				Usage:   "annotate each FILE_ID with its note name (source sans .note), tab-separated",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 0 {
				return fmt.Errorf("usage: snorg [-a <archive-path>] list")
			}
			ids, err := retrieve.List(a.arch)
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
				nd, err := a.arch.ReadNote(id)
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
			res, err := retrieve.Get(a.arch, pageIDs)
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

const queryFilters = "all, note <FILE_ID>, unanalyzed, keyword <regexp>, content <regexp>, starred, date <spec>, not <filter> (inverse)"

func queryCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "query",
		Usage:     "print PAGEIDs of matching pages, one per line (pipe into retrieve/analyze/export); -l/--long annotates them (browse-only, not pipe-safe)",
		ArgsUsage: "<filter> [arg]   (filters: " + queryFilters + ")",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:    "long",
				Aliases: []string{"l"},
				Usage:   "annotate each PAGEID with note, page#, *, headings and #keywords in tab-separated columns (do not pipe downstream)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 1 {
				return fmt.Errorf("usage: snorg [-a <archive-path>] query <filter> [arg]\n  filters: %s", queryFilters)
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
			if !cmd.Bool("long") {
				for _, m := range matches {
					fmt.Println(m.PageID)
				}
				return nil
			}
			return printQueryLong(a.arch, matches)
		},
	}
}

// queryRow is one assembled line of the browse-only `query -l` view.
type queryRow struct {
	pageID   string
	note     string // source filename sans ".note" (FILE_ID fallback)
	number   int    // 1-based page number from note.json placement
	starred  bool
	headings []string // analyzed title names (empty until analyze runs)
	keywords []string // device keyword text (present without analysis)
}

// printQueryLong emits the human-readable, browse-only form of a query result:
// tab-separated columns "<PAGEID>\t<note>\tp<page#>\t<*?>\t<headings ' / '>\t#kw #kw",
// where note is the source filename sans ".note", page# comes from note.json's
// placement, * marks a starred page (empty otherwise), headings are the analyzed
// title names (empty until analyze runs) and keywords (device metadata, present
// without analysis) are rendered as #tags so a fuzzy finder can filter on them.
// PAGEID stays the first whitespace field on purpose (`awk '{print $1}'` extracts
// it for the fzf → serve workflow). Fixed \t separators keep the columns machine-
// splittable (cut -f) regardless of value widths. This is deliberately NOT the
// bare-PAGEID pipe contract, so it is never fed downstream.
func printQueryLong(a *archive.Archive, matches []query.Match) error {
	notes := make(map[string]*archive.NoteDoc)
	for _, m := range matches {
		nd, ok := notes[m.FileID]
		if !ok {
			read, err := a.ReadNote(m.FileID)
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
		pd, err := a.ReadPage(m.FileID, m.PageID)
		if err != nil {
			return fmt.Errorf("note %s page %s: %w", m.FileID, m.PageID, err)
		}
		row := queryRow{pageID: m.PageID, note: name, number: number, starred: pd.Starred}
		for _, t := range pd.Titles {
			if t.Analysis != nil {
				row.headings = append(row.headings, t.Analysis.Name)
			}
		}
		for _, k := range pd.Keywords {
			row.keywords = append(row.keywords, "#"+k.Text)
		}
		star := ""
		if row.starred {
			star = "*"
		}
		fmt.Printf("%s\t%s\tp%d\t%s\t%s\t%s\n",
			row.pageID, row.note, row.number, star,
			strings.Join(row.headings, " / "), strings.Join(row.keywords, " "))
	}
	return nil
}

func queryPredicate(a *archive.Archive, filter string, args []string) (query.Predicate, error) {
	arity := func(n int, usage string) error {
		if len(args) != n {
			return fmt.Errorf("usage: snorg [-a <archive-path>] query %s", usage)
		}
		return nil
	}
	// "not" is a prefix that inverts any filter: it recurses on the rest (so the
	// inner filter's own arg parsing/arity apply verbatim) and negates the result.
	if filter == "not" {
		if len(args) < 1 {
			return nil, fmt.Errorf("usage: snorg [-a <archive-path>] query not <filter> [arg]\n  filters: %s", queryFilters)
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
				return fmt.Errorf("usage: snorg [-a <archive-path>] analyze-edit <PAGEID>")
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
			res, err := retrieve.Get(a.arch, pageIDs)
			if err != nil {
				return err
			}
			out, err := export.Render(res, a.cfg.Export.Template)
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
			res, err := retrieve.Get(a.arch, pageIDs)
			if err != nil {
				return err
			}
			addr := cmd.String("listen")
			pages := 0
			for _, n := range res.Notes {
				pages += len(n.Pages)
			}
			fmt.Fprintf(os.Stderr, "snorg: serving %d page(s) across %d note(s) on http://%s/\n", pages, len(res.Notes), addr)
			return http.ListenAndServe(addr, serve.Handler(a.arch, res.Notes, cmd.Bool("flat")))
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

func migrateCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "migrate",
		Usage:     "upgrade note.json/page JSON to the current schema version (no PAGEIDs and no pipe = whole archive)",
		ArgsUsage: "[PAGEID ...]",
		Action: func(_ context.Context, cmd *cli.Command) error {
			// Selection mirrors serve, but routes to the archive's un-gated
			// migrator (not query): migrate must read the stale grammars it exists
			// to repair, which the gated readers refuse.
			var results []archive.MigrateResult
			var err error
			switch {
			case cmd.Args().Len() > 0:
				results, err = a.arch.MigratePages(cmd.Args().Slice())
			case stdinPiped():
				var ids []string
				if ids, err = readLines(os.Stdin); err == nil {
					results, err = a.arch.MigratePages(ids)
				}
			default:
				results, err = a.arch.MigrateAll()
			}
			if err != nil {
				return err
			}

			migrated, current, failed := 0, 0, 0
			for _, r := range results {
				switch {
				case r.Err != nil:
					failed++
					fmt.Fprintf(os.Stderr, "failed %s %s: %v\n", r.Kind, r.ID, r.Err)
				case r.Outcome == archive.MigrateUpgraded:
					migrated++
					fmt.Printf("%s %s: %s\n", r.Kind, r.ID, r.Outcome)
				default:
					current++
				}
			}
			fmt.Printf("migrated %d, current %d, failed %d of %d file(s) -> schema v%d\n",
				migrated, current, failed, len(results), archive.CurrentSchemaVersion)
			if failed > 0 {
				return fmt.Errorf("%d of %d file(s) failed to migrate", failed, len(results))
			}
			return nil
		},
	}
}
