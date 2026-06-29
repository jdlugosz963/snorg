// Command snorg is the supernote-organizer CLI.
//
//	snorg ingest   <file.note> <archive-path>
//	snorg list     <archive-path>
//	snorg retrieve <archive-path> <FILE_ID>
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jdlugosz963/snorg/internal/archive"
	"github.com/jdlugosz963/snorg/internal/ingest"
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
	default:
		usage()
		return fmt.Errorf("unknown command: %q", args[0])
	}
}

func cmdIngest(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: snorg ingest <file.note> <archive-path>")
	}
	notePath, archivePath := args[0], args[1]
	note, err := ingest.Run(sntool.New(), archive.New(archivePath), notePath)
	if err != nil {
		return err
	}
	fmt.Printf("ingested %s (%d pages) -> %s/%s\n", note.Source, len(note.Pages), archivePath, note.FileID)
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

func usage() {
	fmt.Fprintln(os.Stderr, "usage:\n"+
		"  snorg ingest   <file.note> <archive-path>\n"+
		"  snorg list     <archive-path>\n"+
		"  snorg retrieve <archive-path> <FILE_ID>")
}
