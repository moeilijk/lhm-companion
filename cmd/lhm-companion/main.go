package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/moeilijk/lhm-companion/internal/hwmon"
	"github.com/moeilijk/lhm-companion/internal/nvidia"
	"github.com/moeilijk/lhm-companion/internal/server"
	"github.com/moeilijk/lhm-companion/internal/system"
)

var version = "dev"

func main() {
	var (
		port    = flag.Int("port", envInt("LHM_PORT", 8085), "port to listen on")
		withNv  = flag.Bool("nvidia", nvidia.Available(), "include nvidia-smi GPU readings")
		showVer = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println(version)
		os.Exit(0)
	}

	if *withNv && !nvidia.Available() {
		log.Println("warning: --nvidia specified but nvidia-smi not found")
		*withNv = false
	}

	provide := func() server.Node {
		children := system.ReadAll()

		// Skip the "nvidia" hwmon device when nvidia-smi is active to avoid
		// duplicate GPU temperature readings.
		var hwmonSkip []string
		if *withNv {
			hwmonSkip = []string{"nvidia"}
		}
		children = append(children, hwmon.ReadAll(hwmonSkip...)...)

		if *withNv {
			children = append(children, nvidia.ReadAll()...)
		}

		return server.Node{
			Text:     hwmon.Hostname(),
			ImageURL: "images_icon/computer.png",
			Children: children,
		}
	}

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("lhm-companion %s listening on %s", version, addr)
	if err := http.ListenAndServe(addr, server.New(provide)); err != nil {
		log.Fatal(err)
	}
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscan(v, &n); err != nil {
		return def
	}
	return n
}
