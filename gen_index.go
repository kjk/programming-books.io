package main

import (
	"io"
	"path/filepath"

	"github.com/kjk/u"
)

var (
	indexDestDir string
)

func genIndexGrid(books []*Book, w io.Writer) error {
	d := struct {
		PageCommon
		Books []*Book
	}{
		PageCommon: getPageCommon(),
		Books:      books,
	}
	path := filepath.Join(indexDestDir, "index-grid.html")
	return execTemplate("index-grid.tmpl.html", d, path, w)
}

func genFeedback(dir string, w io.Writer) error {
	path := filepath.Join(dir, "feedback.html")
	d := struct {
		PageCommon
	}{
		PageCommon: getPageCommon(),
	}
	return execTemplate("feedback.tmpl.html", d, path, w)
}

func genAbout(dir string, w io.Writer) error {
	d := getPageCommon()
	path := filepath.Join(dir, "about.html")
	return execTemplate("about.tmpl.html", d, path, w)
}

// generates book for www.programming-books.io
func genIndex(books []*Book, w io.Writer) error {
	leftBooks, rightBooks := splitBooks(books)
	d := struct {
		PageCommon
		Books      []*Book
		LeftBooks  []*Book
		RightBooks []*Book
		NotionURL  string
	}{
		PageCommon: getPageCommon(),
		Books:      books,
		LeftBooks:  leftBooks,
		RightBooks: rightBooks,
		NotionURL:  gitHubBaseURL,
	}

	path := filepath.Join(indexDestDir, "index.html")
	return execTemplate("index2.tmpl.html", d, path, w)
}

func gen404TopLevel(dir string) {
	d := struct {
		PageCommon
		Book *Book
	}{
		PageCommon: getPageCommon(),
	}
	path := filepath.Join(dir, "404.html")
	_ = execTemplateToFileMaybeMust("404.tmpl.html", d, path)
}

func gen404Indexl(dir string) {
	d := struct {
		PageCommon
		Book *Book
	}{
		PageCommon: getPageCommon(),
	}
	path := filepath.Join(dir, "404.html")
	_ = execTemplateToFileMaybeMust("404-index.tmpl.html", d, path)
}

func copyVercelIndexRoutes(dir string) {
	src := "vercel.json"
	dst := filepath.Join(dir, "vercel.json")
	u.CopyFileMust(dst, src)
	logf("Copied '%s' => '%s'\n", src, dst)
}

func genBooksIndex(books []*Book) {
	currBookDir = indexDestDir
	// cache is only valid for the book
	hashToOptimizedURL = map[string]string{}
	u.CreateDirMust(indexDestDir)
	dir := filepath.Join(indexDestDir, "s")
	u.CreateDirMust(dir)
	//copyVercelIndexRoutes(indexDestDir)

	copyCoversMust(indexDestDir)

	_ = genIndex(books, nil)
	_ = genIndexGrid(books, nil)
	gen404Indexl(indexDestDir)
	_ = genAbout(indexDestDir, nil)
	_ = genFeedback(indexDestDir, nil)
}
