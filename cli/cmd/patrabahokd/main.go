// patrabahokd runs two independent listeners against the same database:
//   - the JSON API, bearer-token authenticated, over a Unix domain socket by default
//     (TCP only via explicit opt-in) — for local automation/integration.
//   - the admin web dashboard, session-cookie authenticated, over HTTPS on its own
//     TCP port — for humans, reusing the mail server's own Let's Encrypt certificate.
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
	"github.com/itsrifathridoy/patrabahok/cli/internal/mtasts"
	"github.com/itsrifathridoy/patrabahok/cli/internal/webui"
)

func main() {
	socketPath := flag.String("socket", "/run/patrabahok/api.sock", "Unix socket to listen on for the JSON API")
	tcpAddr := flag.String("tcp", "", "optional TCP address to also/instead listen on for the JSON API, e.g. 127.0.0.1:8991 (opt-in; off by default)")
	dbConfig := flag.String("db-config", "", "path to the MySQL client config (default /etc/patrabahok/mysql-admin.cnf)")
	webAddr := flag.String("web-addr", ":8443", "address for the admin web dashboard to listen on")
	webCert := flag.String("web-cert", "", "TLS certificate (fullchain) for the admin web dashboard; dashboard disabled if empty")
	webKey := flag.String("web-key", "", "TLS private key for the admin web dashboard")
	mtastsAddr := flag.String("mtasts-addr", "", "address to serve MTA-STS policy files (/.well-known/mta-sts.txt) on, e.g. :443; disabled if empty")
	flag.Parse()

	conn, err := db.Open(*dbConfig)
	if err != nil {
		log.Fatalf("patrabahokd: %v", err)
	}
	defer conn.Close()

	apiSrv := api.New(conn)

	var listener net.Listener
	if *tcpAddr != "" {
		listener, err = net.Listen("tcp", *tcpAddr)
		if err != nil {
			log.Fatalf("patrabahokd: listen tcp %s: %v", *tcpAddr, err)
		}
		log.Printf("patrabahokd: API listening on tcp://%s (opt-in mode — ensure this is not exposed beyond localhost)", *tcpAddr)
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
		log.Printf("patrabahokd: API listening on unix://%s", *socketPath)
	}

	go func() {
		if err := apiSrv.Serve(listener); err != nil {
			log.Fatalf("patrabahokd: API serve: %v", err)
		}
	}()

	if *webCert != "" && *webKey != "" {
		webSrv := webui.New(conn)
		go func() {
			if err := webSrv.ServeTLS(*webAddr, *webCert, *webKey); err != nil {
				log.Fatalf("patrabahokd: web serve: %v", err)
			}
		}()
	} else {
		log.Printf("patrabahokd: admin web dashboard disabled (no -web-cert/-web-key given)")
	}

	if *mtastsAddr != "" {
		go func() {
			// Deliberately non-fatal, unlike the API/dashboard listeners above: MTA-STS
			// hosting is opt-in per domain and secondary to core mail service, so a
			// problem binding its port (already open in ufw, but conceivably taken by
			// something else) shouldn't take down the management API and dashboard with it.
			if err := mtasts.ListenAndServeTLS(*mtastsAddr); err != nil {
				log.Printf("patrabahokd: mtasts serve: %v (MTA-STS policy hosting disabled)", err)
			}
		}()
	}

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
