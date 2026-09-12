// netprobe is a disposable TCP/UDP endpoint for the real network lab. It does
// not implement SSH or authenticate users; do not install it as a guest service.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	id := flag.String("id", "probe", "identifier returned in responses")
	portList := flag.String("ports", "22,8080", "comma-separated TCP and UDP ports")
	flag.Parse()
	seen := map[int]bool{}
	for _, item := range strings.Split(*portList, ",") {
		port, err := strconv.Atoi(item)
		if err != nil || port < 1 || port > 65535 {
			log.Fatalf("invalid port %q", item)
		}
		if seen[port] {
			continue
		}
		seen[port] = true
		tcp, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
		if err != nil {
			log.Fatal(err)
		}
		defer tcp.Close()
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprintf(w, "%s tcp %d\n", *id, port)
		})
		server := &http.Server{Handler: handler, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second}
		go func() { _ = server.Serve(tcp) }()
		udp, err := net.ListenPacket("udp4", fmt.Sprintf("0.0.0.0:%d", port))
		if err != nil {
			log.Fatal(err)
		}
		defer udp.Close()
		go func() {
			buffer := make([]byte, 512)
			for {
				n, remote, err := udp.ReadFrom(buffer)
				if err != nil {
					return
				}
				_, _ = udp.WriteTo([]byte(fmt.Sprintf("%s udp %d %s", *id, port, buffer[:n])), remote)
			}
		}()
	}
	log.Printf("probe %s ready on %s", *id, *portList)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
}
