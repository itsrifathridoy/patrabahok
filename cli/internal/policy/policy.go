// Package policy is a Postfix policy delegation service (check_policy_service, see
// https://www.postfix.org/SMTPD_POLICY_README.html) that enforces group posting
// policies at RCPT time, so a rejected sender gets an immediate SMTP error instead of a
// bounce. Postfix asks once per recipient; anything that isn't a group address gets
// DUNNO and continues through the normal restrictions.
//
// Identity: an authenticated session (sasl_username) is trusted as that mailbox. An
// unauthenticated sender is only trusted for addresses outside the hosted domains
// (external group members), because anyone can claim a local envelope sender over port
// 25. Local users must authenticate to post to a restricted group. Unauthenticated
// loopback clients are trusted, matching permit_mynetworks; authenticated ones are
// checked like any other session (webmail submits over loopback).
package policy

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

type Server struct {
	db *sql.DB
}

func New(db *sql.DB) *Server {
	return &Server{db: db}
}

func (s *Server) ListenAndServe(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("policy: listening on %s (group posting policies)", addr)
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go s.serveConn(conn)
	}
}

// serveConn handles one Postfix connection, which carries any number of requests:
// "name=value" lines terminated by an empty line, each answered with "action=...".
func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		attrs := map[string]string{}
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				if !errors.Is(err, io.EOF) {
					log.Printf("policy: read: %v", err)
				}
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if k, v, ok := strings.Cut(line, "="); ok {
				attrs[k] = v
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		action := s.Decide(ctx, Request{
			Sender:        attrs["sender"],
			Recipient:     attrs["recipient"],
			SASLUsername:  attrs["sasl_username"],
			ClientAddress: attrs["client_address"],
		})
		cancel()
		if _, err := fmt.Fprintf(conn, "action=%s\n\n", action); err != nil {
			return
		}
	}
}

type Request struct {
	Sender, Recipient, SASLUsername, ClientAddress string
}

const dunno = "DUNNO"

// Decide returns the Postfix action for one recipient.
func (s *Server) Decide(ctx context.Context, req Request) string {
	recipient := normalize(req.Recipient)
	if recipient == "" {
		return dunno
	}
	var groupID int64
	var policy string
	err := s.db.QueryRowContext(ctx, `SELECT id, post_policy FROM mail_groups WHERE email = ?`, recipient).Scan(&groupID, &policy)
	if errors.Is(err, sql.ErrNoRows) {
		return dunno
	}
	if err != nil {
		log.Printf("policy: group lookup for %s: %v", recipient, err)
		return "DEFER_IF_PERMIT Group policy check temporarily unavailable"
	}
	authenticated := req.SASLUsername != ""
	// Unauthenticated loopback clients are local processes (cron, scripts) and are
	// trusted like permit_mynetworks. Authenticated sessions are always checked, even
	// from loopback, because webmail submits through 127.0.0.1:587 on behalf of
	// whichever user is signed in.
	if policy == "anyone" || (!authenticated && isLoopback(req.ClientAddress)) {
		return dunno
	}

	sender := normalize(req.Sender)
	if authenticated {
		sender = normalize(req.SASLUsername)
	}

	if policy == "organization" {
		if authenticated {
			return dunno
		}
		return reject(recipient, "only people in this organization can post; sign in to your mail account to send")
	}

	if !authenticated {
		if sender == "" {
			return reject(recipient, "only group members can post")
		}
		local, err := s.isHostedAddress(ctx, sender)
		if err != nil {
			log.Printf("policy: domain lookup for %s: %v", sender, err)
			return "DEFER_IF_PERMIT Group policy check temporarily unavailable"
		}
		if local {
			return reject(recipient, "sign in to your mail account to post to this group")
		}
	}

	role, err := s.memberRole(ctx, groupID, sender)
	if err != nil {
		log.Printf("policy: membership lookup for %s in %s: %v", sender, recipient, err)
		return "DEFER_IF_PERMIT Group policy check temporarily unavailable"
	}
	switch policy {
	case "members":
		if role != "" {
			return dunno
		}
		return reject(recipient, "only group members can post")
	case "owners":
		if role == "owner" {
			return dunno
		}
		return reject(recipient, "only group owners can post")
	}
	return dunno
}

func reject(group, why string) string {
	log.Printf("policy: rejected post to %s: %s", group, why)
	return "REJECT 5.7.1 <" + group + ">: " + why
}

func (s *Server) memberRole(ctx context.Context, groupID int64, address string) (string, error) {
	if address == "" {
		return "", nil
	}
	var role string
	err := s.db.QueryRowContext(ctx, `SELECT role FROM mail_group_members WHERE group_id = ? AND member = ?`, groupID, address).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return role, err
}

func (s *Server) isHostedAddress(ctx context.Context, address string) (bool, error) {
	_, domain, ok := strings.Cut(address, "@")
	if !ok {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM virtual_domains WHERE name = ?`, domain).Scan(&n)
	return n > 0, err
}

// normalize lowercases an address and strips a "+tag" extension (recipient_delimiter
// is "+"), matching how Postfix itself resolves group+tag@domain to the group.
func normalize(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	local, domain, ok := strings.Cut(addr, "@")
	if !ok {
		return addr
	}
	if i := strings.IndexByte(local, '+'); i > 0 {
		local = local[:i]
	}
	return local + "@" + domain
}

func isLoopback(addr string) bool {
	ip := net.ParseIP(addr)
	return ip != nil && ip.IsLoopback()
}
