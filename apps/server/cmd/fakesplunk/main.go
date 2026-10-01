// Command fakesplunk serves a fake Splunk REST API for local development.
// Any search containing "err" returns two log lines; others return nothing.
// POST /fake/expire and /fake/restore toggle an expired session.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/falahlaz/find-bugs-web/apps/server/internal/findbugs/splunk/splunktest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8089", "listen address")
	flag.Parse()
	f := splunktest.New()
	f.SetLogs("err", `{"level":"INFO","msg":"request received"}`, `{"level":"ERROR","service":"[ESB]","msg":"ESB timeout after 30s","status":504}`)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /fake/expire", func(w http.ResponseWriter, _ *http.Request) { f.SetExpired(true) })
	mux.HandleFunc("POST /fake/restore", func(w http.ResponseWriter, _ *http.Request) { f.SetExpired(false) })
	mux.Handle("/", f)
	log.Printf("fake splunk on http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
