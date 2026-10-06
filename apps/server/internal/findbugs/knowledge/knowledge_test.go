package knowledge

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFrontmatter(t *testing.T) {
	fm := Frontmatter(strings.NewReader(`---
title: "Checkout gagal: 20011"
summary: Harga "Rp 15,000" ditolak
tags: cobe, checkout, prohibited character
endpoints: [POST /a, "POST /b"]
aliases:
  - one
  - two
---
# body
title: ignored`))
	want := Fields{
		"title":     {"Checkout gagal: 20011"},
		"summary":   {`Harga "Rp 15,000" ditolak`},
		"tags":      {"cobe", "checkout", "prohibited character"},
		"endpoints": {"POST /a", "POST /b"},
		"aliases":   {"one", "two"},
	}
	if !reflect.DeepEqual(fm, want) {
		t.Fatalf("got %#v", fm)
	}
	if len(Frontmatter(strings.NewReader("# no frontmatter\ntitle: x"))) != 0 {
		t.Fatal("parsed a file without frontmatter")
	}
}

func TestListOpenZip(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "svc", "2026-10-01-old", "report.md"), "---\ntitle: Old\ndate: 2026-10-01\nstatus: fixed\n---\n")
	write(t, filepath.Join(dir, "svc", "2026-10-06-new", "report.md"), "---\ntitle: New\ndate: 2026-10-06\ntags: [a, b]\n---\n")
	write(t, filepath.Join(dir, "svc", "2026-10-06-new", "report.html"), "<html>")
	write(t, filepath.Join(dir, "svc", "2026-10-05-html-only", "report.html"), "<html>")
	write(t, filepath.Join(dir, "svc", "INDEX.md"), "# index")
	write(t, filepath.Join(dir, "svc", "api", "GET-x.md"), "flow")
	write(t, filepath.Join(dir, "svc", "empty", "notes.txt"), "x")
	write(t, filepath.Join(dir, "svc", ".obsidian", "app.json"), "{}")
	write(t, filepath.Join(dir, "other", "2026-09-01-x", "report.md"), "no frontmatter")
	outside := filepath.Join(t.TempDir(), "secret.md")
	write(t, outside, "secret")
	os.Symlink(filepath.Dir(outside), filepath.Join(dir, "svc", "escape"))
	os.Symlink(outside, filepath.Join(dir, "svc", "linked.md"))

	list, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range list {
		got = append(got, r.Repo+"/"+r.Slug+"="+r.Title+"|"+r.Date)
	}
	want := []string{"svc/2026-10-06-new=New|2026-10-06", "svc/2026-10-05-html-only=2026-10-05-html-only|2026-10-05",
		"svc/2026-10-01-old=Old|2026-10-01", "other/2026-09-01-x=2026-09-01-x|2026-09-01"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list = %v", got)
	}
	if n := list[0]; !n.HasMD || !n.HasHTML || !reflect.DeepEqual(n.Tags, []string{"a", "b"}) || list[1].HasMD {
		t.Fatalf("flags = %+v / %+v", n, list[1])
	}
	if l, err := List(filepath.Join(dir, "missing")); err != nil || l != nil {
		t.Fatalf("missing dir = %v %v", l, err)
	}

	f, err := Open(dir, "svc", "2026-10-06-new", HTML)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	for _, c := range [][3]string{
		{"svc", "..", MD}, {"..", "svc", MD}, {"svc", "escape", "secret.md"}, {"svc", "escape", MD},
		{"svc", "api", MD}, {"svc", "2026-10-06-new", "../INDEX.md"}, {"svc", "2026-10-05-html-only", MD},
	} {
		if _, err := Open(dir, c[0], c[1], c[2]); err != ErrNotFound {
			t.Errorf("Open(%v) = %v", c, err)
		}
	}

	var buf bytes.Buffer
	if err := WriteZip(&buf, dir, "svc"); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	want = []string{"knowledge/svc/2026-10-01-old/report.md", "knowledge/svc/2026-10-05-html-only/report.html",
		"knowledge/svc/2026-10-06-new/report.html", "knowledge/svc/2026-10-06-new/report.md",
		"knowledge/svc/INDEX.md", "knowledge/svc/api/GET-x.md"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("zip = %v", names)
	}
	if err := WriteZip(&buf, dir, "../x"); err != ErrNotFound {
		t.Fatalf("bad repo zip = %v", err)
	}
	if err := WriteZip(&buf, dir, "nope"); err != ErrNotFound {
		t.Fatalf("missing repo zip = %v", err)
	}
}
