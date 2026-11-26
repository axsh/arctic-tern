// Command voice_demo is a commented Example: browser voice (OpenAI Realtime)
// plus Tern CAWA for Coding Agent execution and turn interrupt.
//
// This is NOT the production Voice Mediator. It exists so humans can hear
// barge-in → interrupt working end-to-end.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	listen := flag.String("listen", ":8787", "HTTP listen address for the demo UI")
	ternURL := flag.String("tern-url", envOr("TERN_URL", ""), "Base URL of a running Tern/CAWA (required)")
	agent := flag.String("agent", envOr("TERN_AGENT", "wayfinder"), "Coding Agent name for CreateSession")
	workDir := flag.String("work-dir", envOr("TERN_WORK_DIR", ""), "Agent work_dir (default: temp dir per call)")
	staticDir := flag.String("static-dir", "", "Directory with index.html / app.js (default: beside this binary's source static/)")
	flag.Parse()

	if *ternURL == "" {
		log.Fatal("TERN_URL or -tern-url is required (point at a running Tern agent-service HTTP base URL)")
	}

	sd := *staticDir
	if sd == "" {
		// Example-friendly default: features/voice_demo/static next to these sources.
		_, file, _, ok := runtime.Caller(0)
		if ok {
			sd = filepath.Join(filepath.Dir(file), "static")
		} else {
			sd = "static"
		}
	}

	mux := NewDemoMux(DemoConfig{
		TernURL:   *ternURL,
		Agent:     *agent,
		WorkDir:   *workDir,
		OpenAIKey: os.Getenv("OPENAI_API_KEY"),
		StaticDir: sd,
	})

	log.Printf("voice_demo listening on http://127.0.0.1%s (tern=%s agent=%s)", *listen, *ternURL, *agent)
	log.Printf("open the URL, click Start call, allow the microphone")
	if err := http.ListenAndServe(*listen, mux); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
