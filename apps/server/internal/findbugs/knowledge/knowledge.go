// Package knowledge reads the trace-report knowledge base: one folder per
// repo, one folder per issue holding report.md (technical) and report.html
// (stakeholder), as written by the trace-report skill:
//
//	<dir>/<repo>/<YYYY-MM-DD>-<slug>/report.{md,html}
//	<dir>/<repo>/INDEX.md, <dir>/<repo>/api/*.md
package knowledge

import (
	"archive/zip"
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	MD   = "report.md"
	HTML = "report.html"
)

// ErrNotFound is returned for unknown or invalid report paths.
var ErrNotFound = errors.New("laporan tidak ditemukan")

var name = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidName reports whether s is a safe repo or report folder name.
func ValidName(s string) bool { return name.MatchString(s) && !strings.Contains(s, "..") }

// Report is one issue folder with its report.md frontmatter.
type Report struct {
	Repo      string
	Slug      string
	Title     string
	Date      string
	Status    string
	Severity  string
	Summary   string
	Tags      []string
	Endpoints []string
	HasMD     bool
	HasHTML   bool
	Updated   time.Time
}

// List returns every report under dir, newest first. A missing dir is empty.
func List(dir string) ([]Report, error) {
	repos, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Report
	for _, rd := range repos {
		if !rd.IsDir() || !ValidName(rd.Name()) {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(dir, rd.Name()))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() || e.Name() == "api" || !ValidName(e.Name()) {
				continue
			}
			if r, ok := read(filepath.Join(dir, rd.Name(), e.Name()), rd.Name(), e.Name()); ok {
				out = append(out, r)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date > out[j].Date
		}
		return out[i].Updated.After(out[j].Updated)
	})
	return out, nil
}

func read(folder, repo, slug string) (Report, bool) {
	r := Report{Repo: repo, Slug: slug}
	for _, f := range []string{MD, HTML} {
		fi, err := os.Stat(filepath.Join(folder, f))
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if f == MD {
			r.HasMD = true
		} else {
			r.HasHTML = true
		}
		if fi.ModTime().After(r.Updated) {
			r.Updated = fi.ModTime()
		}
	}
	if !r.HasMD && !r.HasHTML {
		return r, false
	}
	if r.HasMD {
		if f, err := os.Open(filepath.Join(folder, MD)); err == nil {
			fm := Frontmatter(f)
			f.Close()
			r.Title, r.Date, r.Status = fm.str("title"), fm.str("date"), fm.str("status")
			r.Severity, r.Summary = fm.str("severity"), fm.str("summary")
			r.Tags, r.Endpoints = fm["tags"], fm["endpoints"]
		}
	}
	if r.Title == "" {
		r.Title = slug
	}
	if r.Date == "" && len(slug) >= 10 {
		if _, err := time.Parse("2006-01-02", slug[:10]); err == nil {
			r.Date = slug[:10]
		}
	}
	return r, true
}

// Fields maps a frontmatter key to its values: a scalar is one value, a
// comma-separated string or a YAML list ([a, b] or "- a" lines) is several.
type Fields map[string][]string

func (f Fields) str(k string) string { return strings.Join(f[k], ", ") }

// Frontmatter parses the leading "---" YAML block of a Markdown file. It
// covers the flat key/value shape the trace-report skill writes, not YAML.
func Frontmatter(r io.Reader) Fields {
	out := Fields{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return out
	}
	var last string
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "---" {
			break
		}
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "- ") && last != "" {
			if v := unquote(t[2:]); v != "" {
				out[last] = append(out[last], v)
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, " ") {
			continue
		}
		last = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		list := strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]")
		if list {
			v = v[1 : len(v)-1]
		}
		if !list && last != "tags" && last != "endpoints" {
			if v = unquote(v); v != "" {
				out[last] = []string{v}
			}
			continue
		}
		for _, p := range strings.Split(v, ",") {
			if p = unquote(p); p != "" {
				out[last] = append(out[last], p)
			}
		}
	}
	return out
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}

// Open opens one report file. repo and slug must be plain folder names and
// file report.md or report.html; os.Root keeps symlinks inside dir.
func Open(dir, repo, slug, file string) (*os.File, error) {
	if !ValidName(repo) || !ValidName(slug) || slug == "api" || (file != MD && file != HTML) {
		return nil, ErrNotFound
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, ErrNotFound
	}
	defer root.Close()
	f, err := root.Open(path.Join(repo, slug, file))
	if err != nil {
		return nil, ErrNotFound
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, ErrNotFound
	}
	return f, nil
}

// WriteZip writes the Markdown and HTML files of one repo (or all repos when
// repo is empty) as knowledge/<repo>/..., ready to open as an Obsidian vault.
// Hidden entries and symlinks are skipped.
func WriteZip(w io.Writer, dir, repo string) error {
	if repo != "" && !ValidName(repo) {
		return ErrNotFound
	}
	base := dir
	if repo != "" {
		base = filepath.Join(dir, repo)
		if fi, err := os.Stat(base); err != nil || !fi.IsDir() {
			return ErrNotFound
		}
	}
	zw := zip.NewWriter(w)
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != base && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !d.Type().IsRegular() || (ext != ".md" && ext != ".html") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(fi)
		if err != nil {
			return err
		}
		hdr.Name = path.Join("knowledge", filepath.ToSlash(rel))
		hdr.Method = zip.Deflate
		dst, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		src, err := os.Open(p)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(dst, src)
		return err
	})
	if err != nil {
		return err
	}
	return zw.Close()
}
