package main

import (
	"html/template"
	"path"
	"path/filepath"
	"strings"

	"github.com/kjk/notionapi"
)

// HeadingInfo describes header/sub header
type HeadingInfo struct {
	Text string
	ID   string
}

// MetaValue describes a single page metadata value
// Meta tags are text blocks at the start of a page
// in the format: "$foo bar" or "@foo bar"
// "foo" is the name, "bar" is the value
// Two forms for legacy reasons
// those are not rendered in HTML
type MetaValue struct {
	Key   string
	Value string
}

// ImageMapping keeps track of rewritten image urls (locally cached
// images in notion)
type ImageMapping struct {
	// this is Block.Source from image block
	link string
	// this is path on the disk
	path string
	// this is relative url of the image on disk
	relativeURL string
}

// Page represents a single page in a book
type Page struct {
	NotionPage *notionapi.Page

	Title string
	// reference to parent page, nil if top-level page
	Parent *Page

	Book *Book

	// meta information extracted from page blocks
	NotionID string // notion id in no-dash format

	// for legacy pages this is an id. Might be used for redirects
	//ID              string
	//StackOverflowID string
	//Search          []string // was SearchSynonyms
	Metadata []*MetaValue

	// extracted from embed blocks
	SourceFiles []*SourceFile
	Images      []*ImageMapping

	BodyHTML template.HTML

	// each page can contain sub-pages
	Pages []*Page

	// for generatic a toc, gathers text and id of every h1, h2, h3
	Headings []*HeadingInfo

	// TODO: those should come from notion_cache and downloaded during download
	// step to notion_cache
	images []string

	blockCodeToSourceFile map[string]*SourceFile
}

var knownMetaKeys = map[string]bool{
	"id":          true, // id, obsolete
	"soid":        true, // stack overflow id, obsolete
	"search":      true, // additional search terms
	"score":       true, // stack overflow score
	"draft":       true, // draft is hidden from public view
	"todo":        true, // todo is hidden from public view
	"note":        true, // for leaving notes to myself
	"desc":        true, // for SEO (meant to go into "description" meta tag)
	"description": true, // same as "desc"
	"cleanup":     true,
	"needs":       true, // "needs cleanup"
}

func isKnownMeta(s string) bool {
	return knownMetaKeys[s]
}

func (p *Page) findMeta(key string) string {
	for _, mv := range p.Metadata {
		if mv.Key == key {
			return mv.Value
		}
	}
	return ""
}

func (p *Page) hasMeta(key string) bool {
	for _, mv := range p.Metadata {
		if mv.Key == key {
			return true
		}
	}
	return false
}

func (p *Page) getID() string {
	return p.NotionID
}

func (p *Page) getSearch() []string {
	s := p.findMeta("search")
	if s != "" {
		return nil
	}
	a := strings.Split(s, ",")
	for i, s := range a {
		a[i] = strings.TrimSpace(s)
	}
	return a
}

func (p *Page) isDraft() bool {
	return p.hasMeta("draft")
}

// Body is a temporary alias for BodyHTML
func (p *Page) Body() template.HTML {
	return p.BodyHTML
}

// HTML is a temporary alias for BodyHTML
func (p *Page) HTML() template.HTML {
	return p.BodyHTML
}

// URLRelative url assuming the referal is from a document
// at the same level
func (p *Page) URLRelative() string {
	id := p.NotionID
	title := urlify(p.Title)
	// ${title}-${id}
	return title + "-" + id
}

// URL returns url of the page
func (p *Page) URL() string {
	id := p.NotionID
	title := urlify(p.Title)
	// /${title}-${id}
	return urlJoin(p.Book.URL(), "/"+title+"-"+id)
}

// CanonnicalURL returns full url including host
func (p *Page) CanonnicalURL() string {
	return urlJoin(siteBaseURL, p.URL())
}

// NotionURL returns url to edit 000-index.md document
func (p *Page) NotionURL() string {
	return notionBaseURL + toNoDashID(p.NotionID)
}

func (p *Page) ImageURL(name string) string {
	return path.Join(p.Book.URL(), name)
}

func (p *Page) destImagePath(name string) string {
	return filepath.Join(p.Book.DirOnDisk, name)
}

// PageTitle returns title for the page
// We want this to be unique for SEO purposes
func (p *Page) PageTitle() string {
	var a []string
	for p != nil {
		t := p.Title
		if t != "" {
			a = append(a, t)
		}
		p = p.Parent
	}
	reverseStringSlice(a)
	return strings.Join(a, " / ")
}

func findSourceFileForEmbedURL(page *Page, uri string) *SourceFile {
	for _, f := range page.SourceFiles {
		if f.EmbedURL == uri {
			return f
		}
	}
	return nil
}

// extract sub page information and removes blocks that contain
// this info
func getSubPages(page *notionapi.Page, pageIDToPage map[string]*Page) []*notionapi.Page {
	var res []*notionapi.Page
	toRemove := map[int]bool{}
	for idx, block := range page.Root().Content {
		if block.Type != notionapi.BlockPage {
			continue
		}
		toRemove[idx] = true
		id := toNoDashID(block.ID)
		subPage := pageIDToPage[id]
		panicIf(subPage == nil, "no sub page for id %s", id)
		res = append(res, subPage.NotionPage)
	}
	removeBlocks(page, toRemove)
	return res
}

// returns nil if this is not a meta-value block
// meta-value block is a plain text block in format:
// $key: value
// or:
// @key value
// e.g. `$Id: 59` or `@Draft`
// We strip the '$' or '@' or '%'
func extractMetaValueFromBlock(block *notionapi.Block) *MetaValue {
	if block.Type != notionapi.BlockText {
		return nil
	}
	if len(block.InlineContent) != 1 {
		return nil
	}
	inline := block.InlineContent[0]
	// must be plain text
	if !inline.IsPlain() {
		return nil
	}

	// remove empty lines at the top
	s := strings.TrimSpace(inline.Text)
	if len(s) < 4 {
		return nil
	}
	isMeta := s[0] == '$' || s[0] == '@' || s[0] == '#' || s[0] == '%'
	if !isMeta {
		return nil
	}
	s = strings.TrimSpace(s[1:])
	keyEnd := strings.Index(s, ":")
	if keyEnd == -1 {
		keyEnd = strings.Index(s, " ")
	}
	key := s
	value := ""
	if keyEnd != -1 {
		key = strings.TrimSpace(strings.ToLower(s[:keyEnd]))
		value = strings.TrimSpace(s[keyEnd+1:])
	}
	return &MetaValue{key, value}
}

// remove blocks whose indexes are in toRemove
func removeBlocks(page *notionapi.Page, toRemove map[int]bool) {
	if len(toRemove) == 0 {
		return
	}

	blocks := page.Root().Content
	n := 0
	for i, el := range blocks {
		if toRemove[i] {
			continue
		}
		blocks[n] = el
		n++
	}
	page.Root().Content = blocks[:n]

	ids := page.Root().ContentIDs
	n = 0
	for i, el := range ids {
		if toRemove[i] {
			continue
		}
		ids[n] = el
		n++
	}
	page.Root().ContentIDs = ids
}

// extracts PageMeta and updates Block.Content to remove the blocks that
// contained meta information
func extractMeta(p *Page) {
	page := p.NotionPage
	toRemove := map[int]bool{}
	for idx, block := range page.Root().Content {
		mv := extractMetaValueFromBlock(block)
		if mv == nil {
			break
		}
		page.Root().Content[idx] = nil
		toRemove[idx] = true
		if !isKnownMeta(mv.Key) {
			uri := "https://notion.so/" + toNoDashID(page.ID)
			logf(ctx(), "Unknown meta value '%s' = '%s' in page %s\n", mv.Key, mv.Value, uri)
		}
		p.Metadata = append(p.Metadata, mv)
	}
	removeBlocks(page, toRemove)
}

// recursively build a Page for each notionapi.Page by extracting
// information from notionapi.Page
func bookPageFromNotionPage(book *Book, page *notionapi.Page) *Page {
	id := toNoDashID(page.ID)
	p := book.idToPage[id]
	p.Title = cleanTitle(page.Root().Title)

	extractMeta(p)

	subPages := getSubPages(page, book.idToPage)

	// fmt.Printf("bookPageFromNotionPage: %s %s\n", toNoDashID(page.ID), res.Meta.ID)

	for _, subPage := range subPages {
		bookPage := bookPageFromNotionPage(book, subPage)
		if !isPreview() && bookPage.isDraft() {
			logvf("skipping draft page %s '%s'\n", bookPage.NotionID, bookPage.Title)
			continue
		}
		bookPage.Book = book
		p.Pages = append(p.Pages, bookPage)
	}
	return p
}

func buildBookPages(book *Book) {
	startPageID := book.NotionStartPageID
	page := book.idToPage[startPageID].NotionPage
	panicIf(page.Root().Type != notionapi.BlockPage, "start block is of type '%s' and not '%s'", page.Root().Type, notionapi.BlockPage)
	book.TitleLong = page.Root().Title
	book.RootPage = bookPageFromNotionPage(book, page)
	//evalCodeSnippets(book)
}
