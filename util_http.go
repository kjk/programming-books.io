package main

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"fmt"
	"io"
	"io/ioutil"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kjk/cheatsheets/pkg/server"
)

var mimeTypes = map[string]string{
	// not present in mime.TypeByExtension()
	".txt": "text/plain",
	".exe": "application/octet-stream",
}

func mimeTypeFromFileName(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	ct := mimeTypes[ext]
	if ct == "" {
		ct = mime.TypeByExtension(ext)
	}
	if ct == "" {
		// if all else fails
		ct = "application/octet-stream"
	}
	return ct
}

// can be used for http.Get() requests with better timeouts. New one must be created
// for each Get() request
func newTimeoutClient(connectTimeout time.Duration, readWriteTimeout time.Duration) *http.Client {
	timeoutDialer := func(cTimeout time.Duration, rwTimeout time.Duration) func(net, addr string) (c net.Conn, err error) {
		return func(netw, addr string) (net.Conn, error) {
			conn, err := net.DialTimeout(netw, addr, cTimeout)
			if err != nil {
				return nil, err
			}
			conn.SetDeadline(time.Now().Add(rwTimeout))
			return conn, nil
		}
	}

	return &http.Client{
		Transport: &http.Transport{
			Dial:  timeoutDialer(connectTimeout, readWriteTimeout),
			Proxy: http.ProxyFromEnvironment,
		},
	}
}
func httpPost(uri string, body []byte) ([]byte, error) {
	// default timeout for http.Get() is really long, so dial it down
	// for both connection and read/write timeouts
	timeoutClient := newTimeoutClient(time.Second*120, time.Second*120)
	resp, err := timeoutClient.Post(uri, "", bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("'%s': status code not 200 (%d)", uri, resp.StatusCode)
	}
	return ioutil.ReadAll(resp.Body)
}

func WriteServerFilesToZip(handlers []server.Handler) ([]byte, error) {
	nFiles := 0

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, flate.BestCompression)
	})

	zipWriteFile := func(zw *zip.Writer, name string, data []byte) error {
		fw, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = fw.Write(data)
		return err
	}

	var err error
	writeFile := func(uri string, d []byte) {
		if err != nil {
			return
		}
		name := strings.TrimPrefix(uri, "/")
		err = zipWriteFile(zw, name, d)
		if err != nil {
			return
		}
		sizeStr := formatSize(int64(len(d)))
		if nFiles%128 == 0 {
			logf(ctx(), "WriteServerFilesToZip: %d file '%s' of size %s\n", nFiles+1, name, sizeStr)
		}
		nFiles++
	}
	server.IterContent(handlers, writeFile)

	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func WriteServerFilesToDir(dir string, handlers []server.Handler) (int, int64) {
	nFiles := 0
	totalSize := int64(0)
	dirCreated := map[string]bool{}

	writeFile := func(uri string, d []byte) {
		name := strings.TrimPrefix(uri, "/")
		name = filepath.FromSlash(name)
		path := filepath.Join(dir, name)
		// optimize for writing lots of files
		// I assume that even a no-op os.MkdirAll()
		// might be somewhat expensive
		fileDir := filepath.Dir(path)
		if !dirCreated[fileDir] {
			must(os.MkdirAll(fileDir, 0755))
			dirCreated[fileDir] = true
		}
		err := os.WriteFile(path, d, 0644)
		must(err)
		fsize := int64(len(d))
		totalSize += fsize
		sizeStr := formatSize(fsize)
		if nFiles%256 == 0 {
			logf(ctx(), "WriteServerFilesToDir: file %d '%s' of size %s\n", nFiles+1, path, sizeStr)
		}
		nFiles++
	}
	server.IterContent(handlers, writeFile)
	return nFiles, totalSize
}

func MakeHTTPServer(srv *server.Server) *http.Server {
	panicIf(srv == nil, "must provide srv")
	httpPort := 8080
	if srv.Port != 0 {
		httpPort = srv.Port
	}
	httpAddr := fmt.Sprintf(":%d", httpPort)
	if isWindows() {
		httpAddr = "localhost" + httpAddr
	}

	mainHandler := func(w http.ResponseWriter, r *http.Request) {
		//logf(ctx(), "mainHandler: '%s'\n", r.RequestURI)
		timeStart := time.Now()
		defer func() {
			if p := recover(); p != nil {
				logf(ctx(), "mainHandler: panicked with with %v\n", p)
				http.Error(w, fmt.Sprintf("Error: %v", r), http.StatusInternalServerError)
				logHTTPReq(r, http.StatusInternalServerError, 0, time.Since(timeStart))
				panic(p)
			}
		}()
		uri := r.URL.Path
		serve, _ := srv.FindHandler(uri)
		if serve == nil {
			http.NotFound(w, r)
			logHTTPReq(r, http.StatusNotFound, 0, time.Since(timeStart))
			return
		}
		if serve != nil {
			cw := server.CapturingResponseWriter{ResponseWriter: w}
			serve(&cw, r)
			logHTTPReq(r, cw.StatusCode, cw.Size, time.Since(timeStart))
			return
		}
		http.NotFound(w, r)
		logHTTPReq(r, http.StatusNotFound, 0, time.Since(timeStart))
	}

	httpSrv := &http.Server{
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second, // introduced in Go 1.8
		Handler:      http.HandlerFunc(mainHandler),
	}
	httpSrv.Addr = httpAddr
	return httpSrv
}

// returns function that will wait for SIGTERM signal (e.g. Ctrl-C) and
// shutdown the server
func StartHTTPServer(httpSrv *http.Server) func() {
	logf(ctx(), "Starting server on http://%s'\n", httpSrv.Addr)
	if isWindows() {
		openBrowser(fmt.Sprintf("http://%s", httpSrv.Addr))
	}

	chServerClosed := make(chan bool, 1)
	go func() {
		err := httpSrv.ListenAndServe()
		// mute error caused by Shutdown()
		if err == http.ErrServerClosed {
			err = nil
		}
		must(err)
		logf(ctx(), "trying to shutdown HTTP server\n")
		chServerClosed <- true
	}()

	return func() {
		c := make(chan os.Signal, 2)
		signal.Notify(c, os.Interrupt /* SIGINT */, syscall.SIGTERM)

		sig := <-c
		logf(ctx(), "Got signal %s\n", sig)

		if httpSrv != nil {
			go func() {
				// Shutdown() needs a non-nil context
				_ = httpSrv.Shutdown(ctx())
			}()
			select {
			case <-chServerClosed:
				// do nothing
				logf(ctx(), "server shutdown cleanly\n")
			case <-time.After(time.Second * 5):
				// timeout
				logf(ctx(), "server killed due to shutdown timeout\n")
			}
		}
	}
}

func StartServer(srv *server.Server) func() {
	httpSrv := MakeHTTPServer(srv)
	return StartHTTPServer(httpSrv)
}
