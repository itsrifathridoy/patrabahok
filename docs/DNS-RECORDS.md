# DNS records

The installer prints and saves these to `/root/patrabahok-dns-<domain>.txt` after setup. You can
reprint them any time with `patrabahok dns show <domain>`.

## A record (once per server, not per domain)

```
mail.example.com.   IN  A   <server-ip>
```

Points your mail hostname at the server. Required before TLS issuance will succeed and before
other mail servers can find you via the MX record below.

## MX record (per domain)

```
example.com.   IN  MX  10  mail.example.com.
```

Tells the world where to deliver mail for `example.com`.

## SPF (TXT, per domain)

```
example.com.   IN  TXT   "v=spf1 mx -all"
```

Declares that only hosts listed in your MX record are allowed to send mail as `example.com`.
`-all` (hard fail) is used from the start since the installer only configures one legitimate
sending path (this server) — there's no soft-launch reason to start with `~all` here.

## DKIM (TXT, per domain)

```
mail._domainkey.example.com.   IN  TXT   "v=DKIM1; k=rsa; p=..."
```

The public half of a 2048-bit RSA key Rspamd generates and uses to sign outgoing mail. The
private key lives at `/var/lib/rspamd/dkim/<domain>.mail.key` (root/`_rspamd` readable only).

## DMARC (TXT, per domain)

```
_dmarc.example.com.   IN  TXT   "v=DMARC1; p=none; rua=mailto:you@example.com"
```

Starts at `p=none` (monitor only, no enforcement) — this is deliberate. Once you've confirmed
SPF and DKIM are passing consistently (check the aggregate reports sent to `rua`, or just send
test mail to Gmail/Outlook and inspect headers), move to:

```
_dmarc.example.com.   IN  TXT   "v=DMARC1; p=quarantine; rua=mailto:you@example.com"
```

and eventually `p=reject` once you're confident. Never start at `p=reject` — a misconfiguration
at that point silently drops your own mail at receiving servers.

## MTA-STS (TXT, optional — not fully set up by this installer)

```
_mta-sts.example.com.   IN  TXT   "v=STSv1; id=<timestamp>"
```

This record alone does nothing without also hosting a policy file at
`https://mta-sts.example.com/.well-known/mta-sts.txt` over valid TLS. The installer prints the
record for convenience but does **not** set up that hosting — see [ROADMAP.md](ROADMAP.md).
Skip this unless you're prepared to host the policy file yourself.

## Checking propagation

```
dig +short A mail.example.com @1.1.1.1
dig +short MX example.com @1.1.1.1
dig +short TXT example.com @1.1.1.1
dig +short TXT mail._domainkey.example.com @1.1.1.1
dig +short TXT _dmarc.example.com @1.1.1.1
```
