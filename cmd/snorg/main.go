// Command snorg is the supernote-organizer CLI.
//
//	snorg ingest   [-j N] <file-or-dir> <archive-path>
//	snorg list     <archive-path>
//	snorg retrieve <archive-path> <FILE_ID>
//	snorg query    <archive-path> <filter> [arg]
//	snorg analyze  [-c config.yaml ...] -page-id <PAGEID> <archive-path>
//	snorg export   [-c config.yaml ...] <archive-path> <FILE_ID>
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"

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
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "snorg: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("no command given")
	}
	switch args[0] {
	case "ingest":
		return cmdIngest(args[1:])
	case "list":
		return cmdList(args[1:])
	case "retrieve":
		return cmdRetrieve(args[1:])
	case "query":
		return cmdQuery(args[1:])
	case "analyze":
		return cmdAnalyze(args[1:])
	case "export":
		return cmdExport(args[1:])
	default:
		usage()
		return fmt.Errorf("unknown command: %q", args[0])
	}
}

func cmdIngest(args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	jobs := fs.Int("j", 0, "max concurrent notes (0 = number of CPUs)")
	fs.IntVar(jobs, "jobs", 0, "alias for -j")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: snorg ingest [-j N] <file-or-dir> <archive-path>")
	}
	inputPath, archivePath := fs.Arg(0), fs.Arg(1)

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

	results := ingest.RunMany(sntool.New(), archive.New(archivePath), paths, *jobs)
	failed := 0
	for _, r := range results {
		if r.Err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "failed %s: %v\n", r.Path, r.Err)
			continue
		}
		fmt.Printf("ingested %s (%d pages) -> %s/%s\n", r.Note.Source, len(r.Note.Pages), archivePath, r.Note.FileID)
	}
	if len(paths) > 1 || failed > 0 {
		fmt.Printf("ingested %d, failed %d of %d\n", len(results)-failed, failed, len(results))
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d notes failed", failed, len(results))
	}
	return nil
}

func cmdList(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: snorg list <archive-path>")
	}
	ids, err := retrieve.List(archive.New(args[0]))
	if err != nil {
		return err
	}
	for _, id := range ids {
		fmt.Println(id)
	}
	return nil
}

func cmdRetrieve(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: snorg retrieve <archive-path> <FILE_ID>")
	}
	view, err := retrieve.Get(archive.New(args[0]), args[1])
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// cmdQuery walks the archive and prints the PAGEID of every page matching a
// single filter, one per line. Filters: "keyword <regexp>" and "starred".
func cmdQuery(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: snorg query <archive-path> <filter> [arg]\n" +
			"  filters: keyword <regexp>, starred")
	}
	a := archive.New(args[0])
	var pred query.Predicate
	switch args[1] {
	case "starred":
		if len(args) != 2 {
			return fmt.Errorf("usage: snorg query <archive-path> starred")
		}
		pred = query.Starred
	case "keyword":
		if len(args) != 3 {
			return fmt.Errorf("usage: snorg query <archive-path> keyword <regexp>")
		}
		re, err := regexp.Compile(args[2])
		if err != nil {
			return fmt.Errorf("invalid keyword regexp: %w", err)
		}
		pred = query.Keyword(re)
	default:
		return fmt.Errorf("unknown filter: %q (want: keyword, starred)", args[1])
	}
	matches, err := query.Pages(a, pred)
	if err != nil {
		return err
	}
	for _, m := range matches {
		fmt.Println(m.PageID)
	}
	return nil
}

// cmdAnalyze runs vision-LLM analysis on a single page (by PAGEID) and writes the
// result into that page's <PAGEID>.json under "analysis".
func cmdAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	pageID := fs.String("page-id", "", "PAGEID of the page to analyze (required)")
	var cfgPaths []string
	fs.Func("c", "config YAML file (repeatable; later files override earlier ones)", func(s string) error {
		cfgPaths = append(cfgPaths, s)
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pageID == "" || fs.NArg() != 1 {
		return fmt.Errorf("usage: snorg analyze [-c config.yaml ...] -page-id <PAGEID> <archive-path>")
	}
	cfg, err := config.Load(cfgPaths)
	if err != nil {
		return err
	}
	if err := cfg.ValidateProvider(); err != nil {
		return err
	}
	tr, err := analyze.NewOpenAI(cfg.Provider.Endpoint, cfg.Provider.APIKey, cfg.Provider.Model)
	if err != nil {
		return err
	}
	spec := analyze.Spec{
		Content: cfg.Analysis.Content.Prompt,
		Title:   cfg.Analysis.Titles.Prompt,
		Link:    cfg.Analysis.Links.Prompt,
	}
	for name, t := range cfg.Analysis.Fields {
		spec.Fields = append(spec.Fields, analyze.Field{Name: name, Prompt: t.Prompt})
	}
	return analyze.Page(context.Background(), archive.New(fs.Arg(0)), tr, tr, spec, *pageID)
}

// cmdExport renders an assembled note through the config's pongo2 template to stdout.
func cmdExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	var cfgPaths []string
	fs.Func("c", "config YAML file (repeatable; later files override earlier ones)", func(s string) error {
		cfgPaths = append(cfgPaths, s)
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: snorg export [-c config.yaml ...] <archive-path> <FILE_ID>")
	}
	cfg, err := config.Load(cfgPaths)
	if err != nil {
		return err
	}
	if cfg.Export.Template == "" {
		return fmt.Errorf("export.template is required")
	}
	view, err := retrieve.Get(archive.New(fs.Arg(0)), fs.Arg(1))
	if err != nil {
		return err
	}
	out, err := export.Render(view, cfg.Export.Template)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:\n"+
		"  snorg ingest   [-j N] <file-or-dir> <archive-path>\n"+
		"  snorg list     <archive-path>\n"+
		"  snorg retrieve <archive-path> <FILE_ID>\n"+
		"  snorg query    <archive-path> <filter> [arg]   (filters: keyword <regexp>, starred)\n"+
		"  snorg analyze  [-c config.yaml ...] -page-id <PAGEID> <archive-path>\n"+
		"  snorg export   [-c config.yaml ...] <archive-path> <FILE_ID>")
}
