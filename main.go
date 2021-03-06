package main

import (
	"flag"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kjk/notionapi"
)

type any = interface{}

const (
	dirWwwGenerated = "www_generated"
	httpPort        = 9044
)

var (
	doMinify bool

	// when downloading pages from the server, count total number of
	// downloaded and those from cache
	nTotalDownloaded int32
	nTotalFromCache  int32
)

func isPreview() bool {
	return flgRunServer
}

func initBook(book *Book) {
	book.DirOnDisk = filepath.Join("essential", book.DirShort)
	book.DirCache = filepath.Join("books", book.DirShort, "cache")
	book.NotionCacheDir = filepath.Join(book.DirCache, "notion")
	book.idToPage = map[string]*Page{}
}

var (
	flgRunServer      bool
	flgGistRedownload bool
	// will only download (no eval, no generation)
	flgImportNotion bool
)

func main() {
	var (
		flgGen                bool
		flgBook               string
		flgDownloadGist       string
		flgRunServerProd      bool
		flgCiUpdateFromNotion bool
		// disables notion cache, forcing re-download of notion page
		// even if cached verison on disk exits. Only applies with
		// -import-notion
		flgDisableNotionCache bool
	)

	{
		flag.BoolVar(&flgRunServer, "run", false, "run dev server")
		flag.BoolVar(&flgRunServerProd, "run-prod", false, "run prod server serving www_generated")
		flag.BoolVar(&flgGen, "gen", false, "generate a book and deploy preview")
		flag.StringVar(&flgBook, "book", "", "name of the books e.g. go,python. if not given, all books are used")
		flag.BoolVar(&flgImportNotion, "import-notion", false, "incremental download from notion (no eval, no html generation")
		flag.StringVar(&flgDownloadGist, "download-gist", "", "id of the gist to (re)download. Must also provide a book")
		flag.BoolVar(&flgDisableNotionCache, "no-cache", false, "if true, disables cache for notion (forces re-download of everything)")
		flag.BoolVar(&flgCiUpdateFromNotion, "ci-update-from-notion", false, "incremental download from notion and checkin if there are changes")
		flag.Parse()
	}

	timeStart := time.Now()
	defer func() {
		logf(ctx(), "Downloaded %d pages, %d from cache. Total time: %s\n", nTotalDownloaded, nTotalFromCache, time.Since(timeStart))
	}()

	// ad-hoc, rarely done tasks
	if false {
		genTwitterImagesAndExit()
		return
	}
	if false {
		genSmallCoversAndExit()
		return
	}
	if false {
		optimizeAllImages([]string{"books"})
		return
	}

	notionapi.LogFunc = logsf
	notionapi.PanicOnFailures = true

	if flgDownloadGist != "" {
		book := findBook(flgBook)
		if book == nil {
			logf(ctx(), "-download-gist also requires valid -book, given: '%s'\n", flgBook)
		}
		downloadSingleGist(book, flgDownloadGist)
		return
	}

	booksToProcess := getAllBooks()
	if flgBook != "" {
		bookNames := strings.Split(flgBook, ",")
		booksToProcess = nil
		for _, bookName := range bookNames {
			book := findBook(bookName)
			panicIf(book == nil, "'%s' is not a valid book name", flgBook)
			booksToProcess = append(booksToProcess, book)
		}
	}
	for _, book := range booksToProcess {
		initBook(book)
	}

	if flgRunServer {
		runServerDynamic(booksToProcess)
		return
	}

	if flgRunServerProd {
		runServerProd()
		return
	}

	if flgGen {
		genToDir(booksToProcess, dirWwwGenerated)
		return
	}

	if flgImportNotion {
		policy := notionapi.PolicyDownloadNewer
		if flgDisableNotionCache {
			policy = notionapi.PolicyDownloadAlways
		}
		logf(ctx(), "Downloading %d books\n", len(booksToProcess))
		n := len(booksToProcess)
		for i, book := range booksToProcess {
			downloadBook(book, policy)
			logvf("downloaded book %d out of %d, name: %s, dir: %s\n", i+1, n, book.Title, book.DirShort)
		}
		return
	}

	if flgCiUpdateFromNotion {
		var cmd *exec.Cmd
		logf(ctx(), "Ci: incrementally update from notion, %d books\n", len(booksToProcess))
		{
			// not sure if needed
			cmd = exec.Command("git", "checkout", "master")
			runCmdMust(cmd)
		}

		n := len(booksToProcess)
		// we don't honor -no-cache flag here
		for i, book := range booksToProcess {
			// conserve memory by not keeping downloaded things in memory and forcing GC() frequently
			// not sure if it matters
			bookCopy := *book
			downloadBook(&bookCopy, notionapi.PolicyDownloadNewer)
			logvf("downloaded book %d out of %d, name: %s, dir: %s\n", i+1, n, book.Title, book.DirShort)
			runtime.GC()
		}

		{
			cmd = exec.Command("git", "status")
			s := runCmdMust(cmd)
			if strings.Contains(s, "nothing to commit, working tree clean") {
				// nothing changed so nothing else to do
				logf(ctx(), "Nothing changed, skipping deploy")
				return
			}
		}

		{
			cmd = exec.Command("git", "config", "--global", "user.email", "kkowalczyk@gmail.com")
			runCmdMust(cmd)

		}
		{
			cmd = exec.Command("git", "config", "--global", "user.name", "Krzysztof Kowalczyk")
			runCmdMust(cmd)
		}

		{
			cmd = exec.Command("git", "add", "books")
			runCmdMust(cmd)
		}
		{
			nowStr := time.Now().Format("2006-01-02")
			commitMsg := "ci: update from notion on " + nowStr
			cmd = exec.Command("git", "commit", "-am", commitMsg)
			runCmdMust(cmd)
		}
		{
			cmd = exec.Command("git", "push")
			runCmdMust(cmd)
		}
		return
	}

	flag.Usage()
}

// download a single gist and store in the cache for a given book
func downloadSingleGist(book *Book, gistID string) {
	bookName := book.DirShort
	logf(ctx(), "Downloading gist '%s' and storing in the cache for the book '%s'\n", gistID, bookName)
	cache := loadCache(book)
	gist := gistDownloadMust(gistID)
	didChange := cache.saveGist(gistID, gist.Raw)
	if didChange {
		logf(ctx(), "Saved a new or updated version of gist\n")
		return
	}
	logf(ctx(), "Gist didn't change!\n")
}
