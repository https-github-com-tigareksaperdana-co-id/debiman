// auxserver serves HTTP redirects and cookie handlers.
package main

import (
	"flag"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/Debian/debiman/internal/aux"
	"github.com/Debian/debiman/internal/bundled"
	"github.com/Debian/debiman/internal/commontmpl"
	"github.com/Debian/debiman/internal/redirect"
)

var (
	indexPath = flag.String("index",
		"/srv/man/auxserver.idx",
		"Path to an auxserver index generated by debiman")

	listenAddr = flag.String("listen",
		"localhost:2431",
		"host:port address to listen on")

	injectAssets = flag.String("inject_assets",
		"",
		"If non-empty, a file system path to a directory containing assets to overwrite")

	baseURL = flag.String("base_url",
		"https://manpages.debian.org",
		"Base URL (without trailing slash) to the site. Used where absolute URLs are required, e.g. sitemaps.")
)

// use go build -ldflags "-X main.debimanVersion=<version>" to set the version
var debimanVersion = "HEAD"

func main() {
	flag.Parse()

	log.Printf("debiman auxserver loading index from %q", *indexPath)

	if *injectAssets != "" {
		if err := bundled.Inject(*injectAssets); err != nil {
			log.Fatal(err)
		}
	}

	idx, err := redirect.IndexFromProto(*indexPath)
	if err != nil {
		log.Fatal(err)
	}

	commonTmpls := commontmpl.MustParseCommonTmpls()
	notFoundTmpl := template.Must(commonTmpls.New("notfound").Parse(bundled.Asset("notfound.tmpl")))
	server := aux.NewServer(idx, notFoundTmpl, debimanVersion)

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)
	go func() {
		for _ = range c {
			log.Printf("SIGHUP received, trying to reload index")

			newidx, err := redirect.IndexFromProto(*indexPath)
			if err != nil {
				log.Printf("Could not load new index from %q: %v", *indexPath, err)
				continue
			}

			log.Printf("Loaded %d manpage entries, %d suites, %d languages from new index %q",
				len(newidx.Entries), len(newidx.Suites), len(newidx.Langs), *indexPath)

			if err := server.SwapIndex(newidx); err != nil {
				log.Printf("Swapping index failed: %v", err)
				continue
			}

			log.Printf("Index swapped")
			// Force the garbage collector to return all unused memory to the
			// operating system. Even though, on Linux, unused memory can
			// apparently be reclaimed by the kernel, preemptively returning the
			// memory is less confusing for sysadmins who aren’t intimately
			// familiar with Go’s memory model.
			debug.FreeOSMemory()
		}
	}()

	basePath := commontmpl.BaseURLPath()
	mux := http.NewServeMux()
	mux.HandleFunc("/jump", server.HandleJump)
	mux.HandleFunc("/suggest", server.HandleSuggest)
	mux.HandleFunc("/", server.HandleRedirect)
	http.Handle("/", http.StripPrefix(basePath, mux))

	log.Printf("Loaded %d manpage entries, %d suites, %d languages from index %q",
		len(idx.Entries), len(idx.Suites), len(idx.Langs), *indexPath)

	log.Printf("Starting HTTP listener on %q", *listenAddr)
	log.Fatal(http.ListenAndServe(*listenAddr, nil))
}
