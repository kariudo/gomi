package history

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"time"

	"github.com/babarot/gomi/internal/config"
	"github.com/babarot/gomi/internal/utils"
	"github.com/docker/go-units"
	"github.com/gobwas/glob"
	"github.com/k0kubun/pp/v3"
	"github.com/k1LoW/duration"
	"github.com/rs/xid"
	"github.com/samber/lo"
)

const (
	historyVersion = 1
	historyFile    = "history.json"
)

var (
	gomiPath    = filepath.Join(os.Getenv("HOME"), ".gomi")
	historyPath = filepath.Join(gomiPath, historyFile)
)

// History represents the history of deleted files
type History struct {
	Version int    `json:"version"`
	Files   []File `json:"files"`

	config config.History
	path   string
}

type File struct {
	Name      string    `json:"name"`
	ID        string    `json:"id"`
	RunID     string    `json:"group_id"` // to keep backward compatible
	From      string    `json:"from"`
	To        string    `json:"to"`
	Timestamp time.Time `json:"timestamp"`
}

func New(c config.History) History {
	return History{path: historyPath, config: c}
}

func (h *History) Open() error {
	slog.Debug("opening history file", "path", h.path)

	parentDir := filepath.Dir(h.path)
	if _, err := os.Stat(parentDir); os.IsNotExist(err) {
		slog.Warn("mkdir", "dir", parentDir)
		_ = os.Mkdir(parentDir, 0755)
	}

	if _, err := os.Stat(h.path); os.IsNotExist(err) {
		backupFile := h.path + ".backup"
		slog.Warn("history file not found", "path", h.path)
		if _, err := os.Stat(backupFile); !os.IsNotExist(err) {
			slog.Warn("backup file found! attempting to restore from backup", "path", backupFile)
			err := os.Rename(backupFile, h.path)
			if err != nil {
				return fmt.Errorf("failed to restore history from backup: %w", err)
			}
			slog.Debug("successfully restored history from backup")
		}
	}

	f, err := os.OpenFile(h.path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		slog.Error("err", "error", err)
		return err
	}
	defer f.Close()

	if stat, err := f.Stat(); err == nil && stat.Size() == 0 {
		slog.Warn("history is empty")
		return nil
	}

	if err := json.NewDecoder(f).Decode(&h); err != nil {
		slog.Error("err", "error", err)
		return err
	}

	slog.Debug("history version", "version", h.Version)
	return nil
}

func (h *History) Backup() error {
	backupFile := h.path + ".backup"
	slog.Debug("backing up history", "path", backupFile)
	f, err := os.Create(backupFile)
	if err != nil {
		return err
	}
	defer f.Close()
	h.setVersion()
	return json.NewEncoder(f).Encode(&h)
}

func (h *History) update(files []File) error {
	slog.Debug("updating history file", "path", h.path)
	f, err := os.Create(h.path)
	if err != nil {
		return err
	}
	defer f.Close()
	h.Files = files
	h.setVersion()
	return json.NewEncoder(f).Encode(&h)
}

func (h *History) Save(files []File) error {
	slog.Debug("saving history file", "path", h.path)
	f, err := os.Create(h.path)
	if err != nil {
		return err
	}
	defer f.Close()
	h.Files = append(h.Files, files...)
	h.setVersion()
	return json.NewEncoder(f).Encode(&h)
}

func (h History) Filter() []File {
	// do not overwrite original slices
	// because remove them from history file actually
	// when updating history
	files := h.Files
	files = lo.Reject(files, func(file File, index int) bool {
		return slices.Contains(h.config.Exclude.Files, file.Name)
	})
	files = lo.Reject(files, func(file File, index int) bool {
		for _, pat := range h.config.Exclude.Patterns {
			if regexp.MustCompile(pat).MatchString(file.Name) {
				return true
			}
		}
		for _, g := range h.config.Exclude.Globs {
			if glob.MustCompile(g).Match(file.Name) {
				return true
			}
		}
		return false
	})
	files = lo.Reject(files, func(file File, index int) bool {
		size, err := utils.DirSize(file.To)
		if err != nil {
			return false // false positive
		}
		if s := h.config.Exclude.Size.Min; s != "" {
			min, err := units.FromHumanSize(s)
			if err != nil {
				return false
			}
			if size <= min {
				return true
			}
		}
		if s := h.config.Exclude.Size.Max; s != "" {
			max, err := units.FromHumanSize(s)
			if err != nil {
				return false
			}
			if max <= size {
				return true
			}
		}
		return false
	})
	files = lo.Filter(files, func(file File, index int) bool {
		if period := h.config.Include.Period; period > 0 {
			d, err := duration.Parse(fmt.Sprintf("%d days", period))
			if err != nil {
				slog.Error("failed to parse duration", "error", err)
				return false
			}
			if time.Since(file.Timestamp) < d {
				return true
			}
		}
		return false
	})
	return files
}

func (h *History) Remove(target File) error {
	slog.Debug("deleting file from history file", "path", h.path, "file", target)
	var files []File
	for _, file := range h.Files {
		if file.ID == target.ID {
			continue
		}
		files = append(files, file)
	}
	return h.update(files)
}

func (h *History) setVersion() {
	if h.Version == 0 {
		h.Version = historyVersion
	}
}

func FileInfo(runID string, arg string) (File, error) {
	name := filepath.Base(arg)
	from, err := filepath.Abs(arg)
	if err != nil {
		return File{}, err
	}
	id := xid.New().String()
	now := time.Now()
	return File{
		Name:  name,
		ID:    id,
		RunID: runID,
		From:  from,
		To: filepath.Join(
			gomiPath,
			fmt.Sprintf("%04d", now.Year()),
			fmt.Sprintf("%02d", now.Month()),
			fmt.Sprintf("%02d", now.Day()),
			runID,
			fmt.Sprintf("%s.%s", name, id),
		),
		Timestamp: now,
	}, nil
}

func (f File) String() string {
	p := pp.New()
	p.SetColoringEnabled(false)
	return p.Sprint(f)
}
