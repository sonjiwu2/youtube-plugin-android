package main

import (
	"flag"
	"fmt"
	core "github.com/sonjiwu2/youtube-plugin-android/core"
	"os"
	"os/signal"
	"time"
)

func main() {
	exe := flag.String("ciadpi", "", "path to ciadpi executable")
	fake := flag.String("fake", "../app/src/main/assets/tls_clienthello_www_google_com.bin", "TLS fake path")
	direct := flag.Bool("direct", false, "test CONNECT/DoH without desync")
	duration := flag.Duration("duration", 0, "optional automatic shutdown")
	flag.Parse()
	e, err := core.Start(core.Options{Executable: *exe, FakePath: *fake, DataDir: "bin/state", Profile: "desktop", Direct: *direct, AutoProbe: !*direct})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer e.Stop()
	fmt.Println("HTTP CONNECT proxy: " + e.Address())
	fmt.Println("DNS: DoH only. Ctrl+C to stop.")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	var timer <-chan time.Time
	if *duration > 0 {
		timer = time.After(*duration)
	}
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-timer:
			return
		case <-tick.C:
			fmt.Println(e.StatusJSON())
		}
	}
}
