package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/adrg/xdg"
	"github.com/babarot/gomi/config"
	"github.com/babarot/gomi/inventory"
	"github.com/babarot/gomi/log"
	"github.com/babarot/gomi/ui"
	"github.com/jessevdk/go-flags"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
	"github.com/nxadm/tail"
	"github.com/rs/xid"
	slogmulti "github.com/samber/slog-multi"
	"golang.org/x/sync/errgroup"
)

const (
	appName = "gomi"
)

// These variables are set in build step
var (
	Version  = "unset"
	Revision = "unset"
)

type Option struct {
	Version  bool     `long:"version" description:"Show version"`
	Restore  bool     `short:"b" long:"restore" description:"Restore deleted file"`
	ViewLogs bool     `long:"logs" description:"View logs"`
	Config   string   `long:"config" description:"Path to config file" default:""`
	RmOption RmOption `group:"Dummy Options (compatible with rm)"`
}

// This should be not conflicts with app option
// https://man7.org/linux/man-pages/man1/rm.1.html
type RmOption struct {
	Interactive   bool `short:"i" description:"(dummy) prompt before every removal"`
	Recursive     bool `short:"r" long:"recursive" description:"(dummy) remove directories and their contents recursively"`
	Force         bool `short:"f" long:"force" description:"(dummy) ignore nonexistent files, never prompt"`
	Directory     bool `short:"d" long:"dir" description:"(dummy) remove empty directories"`
	Verbose       bool `short:"v" long:"verbose" description:"(dummy) explain what is being done"`
	OneFileSystem bool `long:"one-file-system" description:"(dummy) when removing a hierarchy recursively, skip any directory\n....... that is on a file system different from that of the\n....... corresponding command line argument"`
}

type CLI struct {
	config    config.Config
	option    Option
	inventory inventory.Inventory
	runID     string
	logFile   string
}

func main() {
	if err := runMain(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", appName, err)
		slog.Error("failed to run cli", "error", err)
		os.Exit(1)
	}
}

var runID = sync.OnceValue(func() string {
	id := xid.New().String()
	return id
})

var logFilePath string

func init() {
	var errs []error
	fp, ok := os.LookupEnv("LOGS_DIRECTORY")
	if !ok {
		var err error
		fp, err = xdg.CacheFile("gomi/log")
		if err != nil {
			errs = append(errs, err)
			fp = "gomi.log"
		}
		logFilePath = fp
		slog.Debug("xdg cache", "dir", fp)
	}

	var fw, cw io.Writer
	if file, err := os.OpenFile(fp, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		fw = file
	} else {
		errs = append(errs, err)
		fw = io.Discard
	}

	if os.Getenv("DEBUG") == "" {
		cw = io.Discard
	} else {
		cw = os.Stderr
	}

	handler := log.NewWrapHandler(
		slog.NewJSONHandler(fw, &slog.HandlerOptions{Level: slog.LevelDebug}),
		func() []slog.Attr {
			return []slog.Attr{
				slog.String("run_id", runID()),
			}
		})

	logger := slog.New(
		slogmulti.Fanout(
			handler,
			tint.NewHandler(cw, &tint.Options{
				Level:      slog.LevelDebug,
				TimeFormat: time.Kitchen,
			}),
		),
	)
	slog.SetDefault(logger)

	if len(errs) > 0 {
		slog.Error("Log setup failed.", log.LogErrAttr(errors.Join(errs...)))
	}
}

func runMain() error {
	defer slog.Debug("finished main function")
	slog.Debug("runMain starts",
		slog.Group(
			"attributes",
			slog.String("version", Version),
			slog.String("revision", Revision),
		),
	)

	var opt Option
	parser := flags.NewParser(&opt, flags.Default)
	parser.Name = appName
	parser.Usage = "[OPTIONS] files..."
	args, err := parser.Parse()
	if err != nil {
		if flags.WroteHelp(err) {
			return nil
		}
		return err
	}

	cfg, err := config.Parse(opt.Config)
	if err != nil {
		return err
	}

	cli := CLI{
		config:    cfg,
		option:    opt,
		inventory: inventory.New(cfg.Inventory),
		runID:     runID(),
		logFile:   logFilePath,
	}
	return cli.Run(args)
}

func (c CLI) Run(args []string) error {
	slog.Debug("cli.Run starts")
	if err := c.inventory.Open(); err != nil {
		return err
	}

	switch {
	case c.option.Version:
		fmt.Fprintf(os.Stdout, "%s %s (%s)\n", appName, Version, Revision)
		return nil
	case c.option.Restore:
		return c.Restore()
	case c.option.ViewLogs:
		return viewLogs(c.logFile)
	default:
	}

	return c.Put(args)
}

func (c CLI) Restore() error {
	files, err := ui.Run(c.inventory.Filter(), c.config.UI)
	if err != nil {
		return err
	}

	var deletedFiles []inventory.File
	var errs []error

	defer func() {
		for _, file := range deletedFiles {
			err := c.inventory.Remove(file)
			if err != nil {
				slog.Error(fmt.Sprintf("removing from inventory failed: %s", file.Name), "error", err)
			}
		}
	}()

	for _, file := range files {
		if _, err := os.Stat(file.From); err == nil {
			// already exists so to prevent to overwrite
			// add id to the end of filename
			// TODO: Ask to overwrite?
			// e.g. using github.com/AlecAivazis/survey
			file.From = file.From + "." + file.ID
		}
		err := os.Rename(file.To, file.From)
		if err != nil {
			errs = append(errs, err)
			slog.Error("failed to restore! file would not be deleted from inventory file", "error", err)
			continue
		}
		deletedFiles = append(deletedFiles, file.File)
	}

	// respect https://github.com/hashicorp/go-multierror
	if len(errs) > 0 {
		lines := []string{fmt.Sprintf("%d errors occurred:", len(errs))}
		for _, err := range errs {
			lines = append(lines, fmt.Sprintf("\t* %s", err))
		}
		lines = append(lines, "\n")
		return errors.New(strings.Join(lines, "\n"))
	}
	return nil
}

func (c CLI) Put(args []string) error {
	if len(args) == 0 {
		return errors.New("too few arguments")
	}

	files := make([]inventory.File, len(args))

	var eg errgroup.Group

	for i, arg := range args {
		i, arg := i, arg // https://golang.org/doc/faq#closures_and_goroutines
		eg.Go(func() error {
			_, err := os.Stat(arg)
			if os.IsNotExist(err) {
				return fmt.Errorf("%s: no such file or directory", arg)
			}
			file, err := inventory.FileInfo(c.runID, arg)
			if err != nil {
				return err
			}

			files[i] = file
			_ = os.MkdirAll(filepath.Dir(file.To), 0777)
			slog.Debug(fmt.Sprintf("moving %q -> %q", file.From, file.To))
			return os.Rename(file.From, file.To)
		})
	}
	defer c.inventory.Save(files)
	defer eg.Wait()

	return eg.Wait()
}

func viewLogs(file string) error {
	shouldFollow := isatty.IsTerminal(os.Stdout.Fd())
	t, err := tail.TailFile(file, tail.Config{Follow: shouldFollow, ReOpen: shouldFollow})
	if err != nil {
		return err
	}
	for line := range t.Lines {
		fmt.Println(line.Text)
	}
	return nil
}
