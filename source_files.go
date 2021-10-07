package main

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// FileDirective describes reulst of parsing a line like:
// `// no output, no playground`
type FileDirective struct {
	FileName     string // :file foo.txt
	NoOutput     bool   // "no output"
	AllowError   bool   // "allow error"
	LineLimit    int    // limit ${n}
	NoPlayground bool   // no playground
	RunCmd       string // :run ${cmd}
	Collection   string // collection

	Glot     bool // :glot, use glot.io to execute the code snippet
	DoOutput bool // :output
}

// SourceFile represents source file. It comes from a code block in
// Notion, replit, file in repository etc.
type SourceFile struct {
	// for debugging, url of page on notion from which this
	// snippet comes from
	NotionOriginURL string

	EmbedURL string

	// full path of the file
	Path string
	// name of the file
	FileName string

	SnippetName string

	// language of the file, detected from name
	Lang string

	// for some files, this is glot.io snippet id
	GlotPlaygroundID string

	PlaygroundURI string

	// optional, extracted from first line of the file
	// allows providing meta-data instruction for this file
	Directive *FileDirective

	// raw content of the code snippet with line endings normalized to '\n'
	// it can either come from code block in Notion or a file on disk
	// or replit etc.
	CodeFull string

	// CodeFull after extracting directive, run cmd at the top
	// and removing :show annotation lines
	// This is the content to execute
	CodeToRun string

	// the part that we want to show i.e. the parts inside
	// :show start, :show end blocks
	LinesToShow []string

	// output of running a file via glot.io
	GlotOutput string
}

// Output returns the output of the execution of the code snippet
func (f *SourceFile) Output() string {
	if f.Directive.NoOutput {
		return ""
	}
	return f.GlotOutput
}

// Sha1 returns sha1 (in hex) of the code snippet
func (f *SourceFile) Sha1() string {
	return sha1HexOfBytes([]byte(f.CodeFull))
}

// CodeToShow returns part of the file tbat we want to show
func (f *SourceFile) CodeToShow() []byte {
	s := strings.Join(f.LinesToShow, "\n")
	return []byte(s)
}

// strip "//" or "#" comment mark from line and return string
// after removing the mark
func stripComment(line string) (string, bool) {
	line = strings.TrimSpace(line)
	s := strings.TrimPrefix(line, "//")
	if s != line {
		return s, true
	}
	s = strings.TrimPrefix(line, "#")
	if s != line {
		return s, true
	}
	return "", false
}

/* Parses a line like:
// no output, no playground, line ${n}, allow error
*/
func parseFileDirective(res *FileDirective, line string) (bool, error) {
	s, ok := stripComment(line)
	if !ok {
		// doesn't start with a comment, so is not a file directive
		return false, nil
	}
	parts := strings.Split(s, ",")
	for _, s := range parts {
		s = strings.TrimSpace(s)
		// directives can also start with ":", to make them more distinct
		startsWithColon := strings.HasPrefix(s, ":")
		s = strings.TrimPrefix(s, ":")
		if s == "glot" {
			res.Glot = true
		} else if s == "output" {
			res.DoOutput = true
		} else if s == "goplay" {
			panic("found goplay")
		} else if s == "no output" || s == "nooutput" {
			res.NoOutput = true
		} else if s == "no playground" || s == "noplayground" {
			res.NoPlayground = true
		} else if s == "allow error" || s == "allow_error" || s == "allowerror" {
			res.AllowError = true
		} else if strings.HasPrefix(s, "name ") {
			// expect: name foo.txt
			rest := strings.TrimSpace(strings.TrimPrefix(s, "name "))
			if len(rest) == 0 {
				return false, fmt.Errorf("parseFileDirective: invalid line '%s'", line)
			}
			res.FileName = rest
		} else if strings.HasPrefix(s, "file ") {
			// expect: file foo.txt
			rest := strings.TrimSpace(strings.TrimPrefix(s, "file "))
			if len(rest) == 0 {
				return false, fmt.Errorf("parseFileDirective: invalid line '%s'", line)
			}
			res.FileName = rest
		} else if strings.HasPrefix(s, "line ") {
			rest := strings.TrimSpace(strings.TrimPrefix(s, "line "))
			n, err := strconv.Atoi(rest)
			if err != nil {
				return false, fmt.Errorf("parseFileDirective: invalid line '%s'", line)
			}
			res.LineLimit = n
		} else if strings.HasPrefix(s, "run ") {
			rest := strings.TrimSpace(strings.TrimPrefix(s, "run "))
			res.RunCmd = rest
			// fmt.Printf("  run:: '%s'\n", res.RunCmd)
		} else if strings.HasPrefix(s, "collection ") {
			rest := strings.TrimSpace(strings.TrimPrefix(s, "collection "))
			res.Collection = rest
		} else {
			// if started with ":" we assume it was meant to be a directive
			// but there was a typo
			if startsWithColon {
				return false, fmt.Errorf("parseFileDirective: invalid line '%s'", line)
			}
			// otherwise we assume this is just a comment
			return false, nil
		}
	}
	return true, nil
}

func extractFileDirectives(lines []string) (*FileDirective, []string, error) {
	directive := &FileDirective{}
	for len(lines) > 0 {
		isDirectiveLine, err := parseFileDirective(directive, lines[0])
		if err != nil {
			return directive, lines, err
		}
		if !isDirectiveLine {
			return directive, lines, nil
		}
		lines = lines[1:]
	}
	return directive, lines, nil
}

// https://www.onlinetool.io/gitoembed/widget?url=https%3A%2F%2Fgithub.com%2Fessentialbooks%2Fbooks%2Fblob%2Fmaster%2Fbooks%2Fgo%2F0020-basic-types%2Fbooleans.go
// to:
// books/go/0020-basic-types/booleans.go
// returns empty string if doesn't conform to what we expect
func gitoembedToRelativePath(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	switch parsed.Host {
	case "www.onlinetool.io", "onlinetool.io":
		// do nothing
	default:
		return ""
	}
	path := parsed.Path
	if path != "/gitoembed/widget" {
		return ""
	}
	uri = parsed.Query().Get("url")
	// https://github.com/essentialbooks/books/blob/master/books/go/0020-basic-types/booleans.go
	parsed, _ = url.Parse(uri)
	if parsed.Host != "github.com" {
		return ""
	}
	path = strings.TrimPrefix(parsed.Path, "/essentialbooks/books/")
	if path == parsed.Path {
		return ""
	}
	// blob/master/books/go/0020-basic-types/booleans.go
	path = strings.TrimPrefix(path, "blob/")
	// master/books/go/0020-basic-types/booleans.go
	// those are branch names. Should I just strip first 2 elements from the path?
	path = strings.TrimPrefix(path, "master/")
	path = strings.TrimPrefix(path, "notion/")
	// books/go/0020-basic-types/booleans.go
	return path
}

// we don't want to show our // :show annotations in snippets
func removeAnnotationLines(lines []string) []string {
	var res []string
	prevWasEmpty := false
	for _, l := range lines {
		if strings.Contains(l, "// :show ") {
			continue
		}
		if len(l) == 0 && prevWasEmpty {
			continue
		}
		prevWasEmpty = len(l) == 0
		res = append(res, l)
	}
	return res
}

func setSourceFileData(sf *SourceFile, data []byte) error {
	sf.CodeFull = string(data)
	lines := dataToLines(data)
	var err error
	directive, lines, _ := extractFileDirectives(lines)
	sf.Directive = directive
	linesToRun := removeAnnotationLines(lines)
	sf.CodeToRun = strings.Join(linesToRun, "\n")
	sf.LinesToShow, err = extractCodeSnippets(lines)
	return err
}
