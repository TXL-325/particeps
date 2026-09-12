package main

import (
	"flag"
	"log"
	"net"
	"net/http"
	"os"

	"particeps/internal/api"
	"particeps/internal/config"
	"particeps/internal/core"
)

func main() {
	cfgPath := flag.String("config", "/etc/particeps/config.yaml", "config file")
	bootstrapOnly := flag.Bool("bootstrap-only", false, "initialize administrator and exit")
	flag.Parse()
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	if v := os.Getenv("PARTICEPS_LISTEN"); v != "" {
		cfg.Listen = v
	}
	app, err := core.Open(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer app.Close()
	password, err := app.BootstrapAdmin()
	if err != nil {
		log.Fatal(err)
	}
	if *bootstrapOnly {
		if password == "" {
			log.Printf("administrator already exists")
		}
		return
	}
	if err := app.Incus.Ready(); err != nil {
		log.Printf("incus not ready: %v", err)
	} else {
		_ = app.Incus.EnsureProject()
		_ = app.Incus.EnsureNetwork(cfg.Network, bridgeAddr(cfg.PrivateIPv4CIDR), true)
		app.RestoreDesiredPower()
	}
	srv := &api.Server{App: app}
	log.Printf("particeps-agent listening on %s", cfg.Listen)
	log.Fatal(http.ListenAndServe(cfg.Listen, srv.Handler()))
}

func bridgeAddr(cidr string) string {
	if cidr == "" {
		cidr = "10.80.0.0/24"
	}
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		return "10.80.0.1/24"
	}
	ip = ip.To4()
	if ip == nil {
		return cidr
	}
	ip[3] = 1
	ones, _ := n.Mask.Size()
	return ip.String() + "/" + itoa(ones)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
