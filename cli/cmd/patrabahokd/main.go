// patrabahokd runs four independent listeners against the same database:
//   - the JSON API, bearer-token authenticated, over a Unix domain socket by default
//     (TCP only via explicit opt-in) — for local automation/integration.
//   - the admin web dashboard, session-cookie authenticated, over HTTPS on its own
//     TCP port — for humans, reusing the mail server's own Let's Encrypt certificate.
//   - a loopback Postfix policy service enforcing group posting policies (internal/policy).
//   - the public HTTPS listener on :443 (internal/frontdoor): MTA-STS policy files, the
//     per-domain public API (domain API keys), and a reverse proxy to the webmail.
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
	"github.com/itsrifathridoy/patrabahok/cli/internal/frontdoor"
	"github.com/itsrifathridoy/patrabahok/cli/internal/policy"
	"github.com/itsrifathridoy/patrabahok/cli/internal/publicapi"
	"github.com/itsrifathridoy/patrabahok/cli/internal/webui"
)

func main() {
	socketPath := flag.String("socket", "/run/patrabahok/api.sock", "Unix socket to listen on for the JSON API")
	tcpAddr := flag.String("tcp", "", "optional TCP address to also/instead listen on for the JSON API, e.g. 127.0.0.1:8991 (opt-in; off by default)")
	dbConfig := flag.String("db-config", "", "path to the MySQL client config (default /etc/patrabahok/mysql-admin.cnf)")
	webAddr := flag.String("web-addr", ":8443", "address for the admin web dashboard to listen on")
	webCert := flag.String("web-cert", "", "TLS certificate (fullchain) for the admin web dashboard; dashboard disabled if empty")
	webKey := flag.String("web-key", "", "TLS private key for the admin web dashboard")
	httpsAddr := flag.String("https-addr", "", "address for the public HTTPS listener (MTA-STS, public per-domain API, webmail proxy), e.g. :443; disabled if empty")
	mtastsAddr := flag.String("mtasts-addr", "", "deprecated alias for -https-addr")
	policyAddr := flag.String("policy-addr", "", "loopback address for the Postfix policy service that enforces group posting policies, e.g. 127.0.0.1:10040; disabled if empty")
	webmailUpstream := flag.String("webmail-upstream", "", "loopback URL of the webmail backend to proxy on the public listener, e.g. http://127.0.0.1:8081; no webmail if empty")
	flag.Parse()

	conn, err := db.Open(*dbConfig)
	if err != nil {
		log.Fatalf("patrabahokd: %v", err)
	}
	defer conn.Close()

	apiSrv := api.New(conn)
	publicAPI := publicapi.New(conn)

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
		webSrv.MountAPI(publicAPI)
		go func() {
			if err := webSrv.ServeTLS(*webAddr, *webCert, *webKey); err != nil {
				log.Fatalf("patrabahokd: web serve: %v", err)
			}
		}()
	} else {
		log.Printf("patrabahokd: admin web dashboard disabled (no -web-cert/-web-key given)")
	}

	if *policyAddr != "" {
		go func() {
			// Fatal: Postfix is configured to consult this service, and while it is down
			// every message to a group is deferred. Exiting lets systemd restart us.
			if err := policy.New(conn).ListenAndServe(*policyAddr); err != nil {
				log.Fatalf("patrabahokd: policy service: %v", err)
			}
		}()
	}

	if *httpsAddr == "" {
		*httpsAddr = *mtastsAddr
	}
	if *httpsAddr != "" {
		cfg := frontdoor.Config{Addr: *httpsAddr, WebmailUpstream: *webmailUpstream}
		if *webCert != "" && *webKey != "" {
			cfg.HostCert = webui.CertSource(*webCert, *webKey)
			cfg.API = publicAPI
		} else {
			log.Printf("patrabahokd: public API and webmail disabled on %s (no -web-cert/-web-key given)", *httpsAddr)
		}
		go func() {
			// Deliberately non-fatal, unlike the API/dashboard listeners above: a problem
			// binding :443 (open in ufw, but conceivably taken by something else) shouldn't
			// take down the management API and dashboard with it.
			if err := frontdoor.ListenAndServeTLS(cfg); err != nil {
				log.Printf("patrabahokd: public https serve: %v (MTA-STS, public API and webmail unavailable)", err)
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
