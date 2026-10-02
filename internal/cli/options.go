package cli

import (
	"errors"
	"flag"
	"fmt"
)

type CommonOptions struct {
	Input, As, Run, Model, StateField                        string
	Workers, ChunkSize, MaxRecordBytes, MaxRecords, MaxBytes int
}

func BindCommon(fs *flag.FlagSet, command string, live, collection bool) *CommonOptions {
	o := &CommonOptions{Input: "jsonl", As: command, Workers: 4, ChunkSize: 32, MaxRecordBytes: 1 << 20, MaxRecords: 10000, MaxBytes: 64 << 20}
	fs.StringVar(&o.Input, "input", o.Input, "input adapter: jsonl or text")
	fs.StringVar(&o.As, "as", o.As, "unique name for appended run")
	fs.IntVar(&o.MaxRecordBytes, "max-record-bytes", o.MaxRecordBytes, "maximum input record bytes")
	if live {
		fs.StringVar(&o.Model, "model", "", "requested model override")
		fs.IntVar(&o.Workers, "workers", o.Workers, "maximum active requests")
		fs.IntVar(&o.ChunkSize, "chunk-size", o.ChunkSize, "records processed before ordered emission")
		if command == "judge" || command == "check" {
			fs.StringVar(&o.StateField, "state-field", "", "top-level data field used as state")
		}
	}
	if !live || command == "rank" || command == "pack" {
		fs.StringVar(&o.Run, "run", "", "saved source run; default latest")
	}
	if collection {
		fs.IntVar(&o.MaxRecords, "max-records", o.MaxRecords, "maximum collection records")
		fs.IntVar(&o.MaxBytes, "max-bytes", o.MaxBytes, "maximum collection input bytes")
	}
	return o
}
func ParseFlags(fs *flag.FlagSet, args []string) error {
	// flag invokes Usage for both help and invalid arguments. Suppress it
	// while parsing, then show the user-facing help only for ErrHelp.
	usage := fs.Usage
	fs.Usage = func() {}
	err := fs.Parse(args)
	fs.Usage = usage
	if err != nil {
		if errors.Is(err, flag.ErrHelp) && usage != nil {
			usage()
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return nil
}
func ValidateCommon(o CommonOptions) error {
	if o.Input != "jsonl" && o.Input != "text" {
		return errors.New("input must be jsonl or text")
	}
	if o.As == "" {
		return errors.New("as cannot be empty")
	}
	if o.Workers < 1 || o.ChunkSize < 1 || o.MaxRecordBytes < 1 || o.MaxRecords < 1 || o.MaxBytes < 1 {
		return errors.New("workers, chunk-size and limits must be positive")
	}
	// Reserve scanner delimiter capacity without integer overflow.
	if o.MaxBytes > int(^uint(0)>>1)-1 {
		return errors.New("max-bytes is too large")
	}
	if o.MaxRecordBytes > int(^uint(0)>>1)-2 {
		return errors.New("max-record-bytes is too large")
	}
	return nil
}
func (a *App) flags(command string) *flag.FlagSet {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(a.Err)
	fs.Usage = func() {
		fmt.Fprintf(a.Out, "Usage: decide %s [options] (experimental)\n", command)
		fs.SetOutput(a.Out)
		fs.PrintDefaults()
		fs.SetOutput(a.Err)
	}
	return fs
}
func (a *App) parse(fs *flag.FlagSet, args []string, o *CommonOptions) (int, bool) {
	if err := ParseFlags(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, false
		}
		return a.fail(err), false
	}
	if err := ValidateCommon(*o); err != nil {
		return a.fail(err), false
	}
	return 0, true
}
func (a *App) fail(err error) int {
	if err != nil {
		fmt.Fprintln(a.Err, redact(err.Error()))
	}
	return 2
}
