package main

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// run optipng in parallel
func optimizeWithOptipng(path string) {
	logf(ctx(), "Optimizing '%s'\n", path)
	cmd := exec.Command("optipng", "-o5", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		// it's ok if fails. some jpeg images are saved as .png
		// which trips it
		logf(ctx(), "optipng failed with '%s'\n", err)
	}
}

func maybeOptimizeImage(path string) {
	ext := filepath.Ext(path)
	ext = strings.ToLower(ext)
	switch ext {
	// TODO: for .gif requires -snip
	case ".png", ".tiff", ".tif", "bmp":
		optimizeWithOptipng(path)
	}
}

func optimizeAllImages() {
	// verify we have optipng installed
	cmd := exec.Command("optipng", "-h")
	err := cmd.Run()
	panicIf(err != nil, "optipng is not installed")

	dirsToVisit := []string{"."}
	for len(dirsToVisit) > 0 {
		dir := dirsToVisit[0]
		dirsToVisit = dirsToVisit[1:]
		files, err := ioutil.ReadDir(dir)
		must(err)
		for _, f := range files {
			name := f.Name()
			path := filepath.Join(dir, name)
			if f.IsDir() {
				if name == "www" || name == "covers" || name == "tmpl" {
					// assume already optimized
				} else {
					dirsToVisit = append(dirsToVisit, path)
				}
				continue
			}
			maybeOptimizeImage(path)
		}
	}
}
