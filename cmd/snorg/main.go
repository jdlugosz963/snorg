// Command snorg is the supernote-organizer CLI. The archive path is a required
// global flag (-a/--archive); the merged config (archive config.yaml, overridden
// by -c files) is loaded once in the root Before hook and shared by every command:
//
//	snorg -a <archive-path> [-c config.yaml ...] [--no-archive-config] <command> [command flags] [args]
//
//	snorg -a <archive-path> ingest [-j N] <file-or-dir>
//	snorg -a <archive-path> list
//	snorg -a <archive-path> retrieve <FILE_ID>
//	snorg -a <archive-path> query <filter> [arg]
//	snorg -a <archive-path> analyze [--force] [PAGEID ...]
//	snorg -a <archive-path> export <FILE_ID>
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/jdlugosz963/snorg/internal/analyze"
	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/config"
	"github.com/jdlugosz963/snorg/internal/export"
	"github.com/jdlugosz963/snorg/internal/ingest"
	"github.com/jdlugosz963/snorg/internal/query"
	"github.com/jdlugosz963/snorg/internal/retrieve"
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
		exportCmd(a),
	}
}

const commandNames = "ingest, list, retrieve, query, analyze, export"

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
		Usage:     "print the assembled note as JSON (the read interface)",
		ArgsUsage: "<FILE_ID>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("usage: snorg -a <archive-path> retrieve <FILE_ID>")
			}
			view, err := retrieve.Get(a.arch, cmd.Args().Get(0))
			if err != nil {
				return err
			}
			b, err := json.MarshalIndent(view, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		},
	}
}

const queryFilters = "all, note <FILE_ID>, unanalyzed, keyword <regexp>, starred"

func queryCmd(a *app) *cli.Command {
	return &cli.Command{
		Name:      "query",
		Usage:     "print PAGEIDs of matching pages, one per line (pipe into analyze)",
		ArgsUsage: "<filter> [arg]   (filters: " + queryFilters + ")",
		Action: func(_ context.Context, cmd *cli.Command) error {
			args := cmd.Args().Slice()
			if len(args) < 1 {
				return fmt.Errorf("usage: snorg -a <archive-path> query <filter> [arg]\n  filters: %s", queryFilters)
			}
			pred, err := queryPredicate(args[0], args[1:])
			if err != nil {
				return err
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

func queryPredicate(filter string, args []string) (query.Predicate, error) {
	arity := func(n int, usage string) error {
		if len(args) != n {
			return fmt.Errorf("usage: snorg -a <archive-path> query %s", usage)
		}
		return nil
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
	default:
		return nil, fmt.Errorf("unknown filter: %q (want: %s)", filter, queryFilters)
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
			cfg := a.cfg
			if err := cfg.ResolveAPIKey(); err != nil {
				return err
			}
			if err := cfg.ValidateProvider(); err != nil {
				return err
			}
			pageIDs := cmd.Args().Slice()
			if len(pageIDs) == 0 {
				var err error
				if pageIDs, err = readLines(os.Stdin); err != nil {
					return err
				}
				if len(pageIDs) == 0 {
					return fmt.Errorf("no PAGEIDs given (arguments or stdin lines)")
				}
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
			failed := 0
			for _, pageID := range pageIDs {
				outcome, err := analyze.Page(ctx, a.arch, tr, tr, spec, pageID, cmd.Bool("force"))
				if err != nil {
					failed++
					fmt.Fprintf(os.Stderr, "failed %s: %v\n", pageID, err)
					continue
				}
				fmt.Printf("%s: %s\n", pageID, outcome)
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d pages failed", failed, len(pageIDs))
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
		Usage:     "render the retrieved note through the config's pongo2 template to stdout",
		ArgsUsage: "<FILE_ID>",
		Action: func(_ context.Context, cmd *cli.Command) error {
			if cmd.Args().Len() != 1 {
				return fmt.Errorf("usage: snorg -a <archive-path> export <FILE_ID>")
			}
			if a.cfg.Export.Template == "" {
				return fmt.Errorf("export.template is required")
			}
			view, err := retrieve.Get(a.arch, cmd.Args().Get(0))
			if err != nil {
				return err
			}
			out, err := export.Render(view, a.cfg.Export.Template)
			if err != nil {
				return err
			}
			fmt.Print(out)
			return nil
		},
	}
}
