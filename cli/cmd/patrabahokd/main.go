// patrabahokd is the local API daemon: a bearer-token authenticated JSON API over a
// Unix domain socket (by default), exposing the same operations as the patrabahok
// CLI. Intended for local automation/integration, not remote/network use — hence
// the Unix-socket default; TCP is available only via explicit opt-in.
package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"github.com/itsrifathridoy/patrabahok/cli/internal/api"
	"github.com/itsrifathridoy/patrabahok/cli/internal/db"
)

func main() {
	socketPath := flag.String("socket", "/run/patrabahok/api.sock", "Unix socket to listen on")
	tcpAddr := flag.String("tcp", "", "optional TCP address to also/instead listen on, e.g. 127.0.0.1:8991 (opt-in; off by default)")
	dbConfig := flag.String("db-config", "", "path to the MySQL client config (default /etc/patrabahok/mysql-admin.cnf)")
	flag.Parse()

	conn, err := db.Open(*dbConfig)
	if err != nil {
		log.Fatalf("patrabahokd: %v", err)
	}
	defer conn.Close()

	srv := api.New(conn)

	var listener net.Listener
	if *tcpAddr != "" {
		listener, err = net.Listen("tcp", *tcpAddr)
		if err != nil {
			log.Fatalf("patrabahokd: listen tcp %s: %v", *tcpAddr, err)
		}
		log.Printf("patrabahokd: listening on tcp://%s (opt-in mode — ensure this is not exposed beyond localhost)", *tcpAddr)
	} else {
		if err := os.MkdirAll(dirOf(*socketPath), 0o755); err != nil {
			log.Fatalf("patrabahokd: %v", err)
		}
		listener, err = api.ListenUnix(*socketPath, 0o660)
		if err != nil {
			log.Fatalf("patrabahokd: listen unix %s: %v", *socketPath, err)
		}
		if grp, err := user.LookupGroup("patrabahok"); err == nil {
			if gid, err := strconv.Atoi(grp.Gid); err == nil {
				_ = os.Chown(*socketPath, -1, gid)
			}
		}
		log.Printf("patrabahokd: listening on unix://%s", *socketPath)
	}

	go func() {
		if err := srv.Serve(listener); err != nil {
			log.Fatalf("patrabahokd: serve: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("patrabahokd: shutting down")
	_ = listener.Close()
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}
