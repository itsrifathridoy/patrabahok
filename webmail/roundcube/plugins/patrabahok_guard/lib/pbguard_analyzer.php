<?php

/**
 * patrabahok_guard: phishing / sender-trust analysis of one message.
 *
 * Everything here is local string work on headers and body (no DNS or HTTP), so it
 * is safe to run on every message view.
 *
 * Copyright (C) patrabahok contributors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <https://www.gnu.org/licenses/>.
 */
class pbguard_analyzer
{
    /** Credential-lure / urgency phrases (lowercase). */
    const LURES = [
        'verify your account', 'verify your identity', 'verify your email', 'verify your mailbox', 'confirm your identity',
        'confirm your account', 'confirm your password', 'validate your account', 'validate your mailbox', 'password expires',
        'password will expire', 'password has expired', 'password expired', 'account suspended', 'account has been suspended',
        'account will be suspended', 'has been suspended', 'temporarily suspended', 'account locked', 'account has been locked',
        'account will be closed', 'account will be deactivated', 'unusual sign-in', 'unusual sign in', 'unusual login',
        'unusual activity', 'suspicious activity', 'suspicious login', 'update payment', 'update your payment',
        'update your billing', 'payment was declined', 'payment failed', 'billing information', 'mailbox is full',
        'mailbox quota', 'storage is full', 'storage full', 'urgent action required', 'action required', 'within 24 hours',
        'within 48 hours', 'reactivate your', 'unlock your account', 'restore access', 'restore your account',
        'click here to verify', 'login to continue', 'sign in to keep', 'avoid suspension', 'avoid deactivation',
        'final notice', 'wire transfer', 'gift card', 'অ্যাকাউন্ট যাচাই', 'যাচাই করুন', 'অ্যাকাউন্ট বন্ধ', 'পাসওয়ার্ড',
    ];

    /** Rspamd symbols that mean phishing or spoofing. */
    const RSPAMD_DANGER = '/\b(PHISHING|PH_SURBL_MULTI|PHISHED_[A-Z_]+|DMARC_POLICY_REJECT|SPOOF_[A-Z_]+|CLAM_VIRUS)\b/';

    /** Common TLDs that make bare link text ("paypal.com") count as a domain. */
    const TEXT_TLDS = ['com', 'net', 'org', 'edu', 'gov', 'info', 'biz', 'app', 'xyz', 'online', 'site', 'top', 'club', 'shop',
        'store', 'live', 'link', 'click', 'cloud', 'dev', 'page', 'support', 'help', 'services', 'center', 'email', 'website',
        'tech', 'today', 'world', 'news', 'bank', 'finance', 'money', 'pay', 'secure', 'login', 'account', 'icu', 'buzz', 'cyou', 'mil'];

    /** @var callable function ($key, $vars = []) returning a localized string */
    private $t;

    /** @var string[] registrable domains of the user's own addresses */
    private $own;

    public $verified = false;

    public $from_reg = '';

    public function __construct(callable $translate, array $own_domains)
    {
        $this->t = $translate;
        $this->own = array_values(array_unique(array_filter(array_map(['pbguard_domain', 'registrable'], $own_domains))));
    }

    /**
     * Parse Authentication-Results. Only the topmost header is used: it is the one our
     * own MTA (Rspamd) added, and Rspamd removes any copy the sender brought along.
     *
     * @return array ['spf' => ..., 'dkim' => ..., 'dmarc' => ...] values: pass|fail|softfail|none|...|null
     */
    public static function parse_auth($ar)
    {
        $res = ['spf' => null, 'dkim' => null, 'dmarc' => null];
        if (is_array($ar)) {
            $ar = reset($ar);
        }

        $ar = strtolower((string) $ar);
        if ($ar === '') {
            return $res;
        }

        if (preg_match_all('/(?:^|[;\s])(spf|dkim|dmarc)\s*=\s*([a-z]+)/', $ar, $m, PREG_SET_ORDER)) {
            foreach ($m as $hit) {
                [$all, $method, $value] = $hit;
                // dkim: any passing signature counts
                if ($res[$method] === null || ($method == 'dkim' && $value == 'pass')) {
                    $res[$method] = $value;
                }
            }
        }

        return $res;
    }

    /**
     * Extract links from an HTML or plain-text body.
     *
     * @return array list of ['href' => string, 'text' => string]
     */
    public static function extract_links($body, $is_html)
    {
        $links = [];
        $body = (string) $body;
        if (strlen($body) > 2000000) {
            $body = substr($body, 0, 2000000);
        }

        if ($is_html) {
            if (preg_match_all('/<a\b([^>]*)>(.*?)<\/a\s*>/is', $body, $m, PREG_SET_ORDER)) {
                foreach ($m as $hit) {
                    $href = self::attr($hit[1], 'href');
                    if ($href === null) {
                        continue;
                    }
                    $links[] = ['href' => $href, 'text' => self::text_of($hit[2])];
                    if (count($links) >= 500) {
                        break;
                    }
                }
            }
        } else {
            if (preg_match_all('#\bhttps?://[^\s<>"\'\)\]]+#i', $body, $m)) {
                foreach (array_slice($m[0], 0, 500) as $url) {
                    $url = rtrim($url, '.,;:!?');
                    $links[] = ['href' => $url, 'text' => $url];
                }
            }
        }

        return $links;
    }

    /** Value of an attribute inside a tag's attribute string, entity-decoded. */
    public static function attr($attrs, $name)
    {
        if (preg_match('/(?:^|\s)' . $name . '\s*=\s*(?:"([^"]*)"|\'([^\']*)\'|([^\s>]+))/i', $attrs, $m)) {
            $v = $m[1] !== '' ? $m[1] : (isset($m[2]) && $m[2] !== '' ? $m[2] : ($m[3] ?? ''));
            return trim(html_entity_decode($v, ENT_QUOTES | ENT_HTML5, 'UTF-8'));
        }

        return null;
    }

    public static function text_of($html)
    {
        $t = html_entity_decode(strip_tags((string) $html), ENT_QUOTES | ENT_HTML5, 'UTF-8');
        $t = preg_replace('/[\s\x{00a0}\x{200b}\x{200c}\x{200d}\x{feff}]+/u', ' ', $t);

        return trim((string) $t);
    }

    /** Host of an http(s) URL (normalized), or null for other schemes / relative links. */
    public static function url_host($url, &$userinfo = null)
    {
        $url = trim((string) $url);
        $userinfo = false;
        if (!preg_match('#^(?:https?:)?//([^/?\#]*)#i', $url, $m)) {
            return null;
        }

        $auth = $m[1];
        if (strpos($auth, '@') !== false) {
            $userinfo = true;
            $auth = substr($auth, strrpos($auth, '@') + 1);
        }

        $auth = rawurldecode($auth);
        if (preg_match('/^\[([^\]]+)\]/', $auth, $mm)) {
            return pbguard_domain::normalize($mm[1]);
        }

        $auth = preg_replace('/:\d*$/', '', $auth);

        return pbguard_domain::normalize($auth);
    }

    /** Domain written as the visible text of a link, or null if the text is not a URL/domain. */
    public static function text_host($text)
    {
        $text = trim($text, " \t<>()[]\"'");
        if ($text === '' || strlen($text) > 2048 || preg_match('/\s/', $text)) {
            return null;
        }

        if (preg_match('#^(?:https?:)?//#i', $text)) {
            return self::url_host($text);
        }

        if (stripos($text, 'mailto:') === 0 || strpos($text, '@') !== false) {
            return null;
        }

        if (preg_match('#^((?:[\p{L}\p{N}](?:[\p{L}\p{N}\-]*[\p{L}\p{N}])?\.)+([\p{L}]{2,63}))(?:[:/?\#].*)?$#u', $text, $m)) {
            $tld = mb_strtolower($m[2]);
            if (stripos($text, 'www.') === 0 || strlen($tld) == 2 || in_array($tld, self::TEXT_TLDS)) {
                return pbguard_domain::normalize($m[1]);
            }
        }

        return null;
    }

    /**
     * Risk of one link.
     *
     * @return array ['level' => danger|warn|none, 'host' => string, 'reasons' => string[]]
     */
    public function link_risk($href, $text)
    {
        $out = ['level' => 'none', 'host' => '', 'reasons' => []];
        $host = self::url_host($href, $userinfo);
        if ($host === null || $host === '') {
            return $out;
        }

        $out['host'] = $host;
        $reg = pbguard_domain::registrable($host);
        $danger = [];
        $warn = [];

        if ($userinfo) {
            $danger[] = $this->t('r_userinfo', ['host' => $host]);
        }

        if (pbguard_domain::is_ip($host)) {
            $danger[] = $this->t('r_ip', ['host' => $host]);
        } else {
            if (preg_match('/(^|\.)xn--/', $host)) {
                $danger[] = $this->t('r_punycode', ['host' => $host]);
            } elseif (pbguard_domain::mixed_script($host)) {
                $danger[] = $this->t('r_mixedscript', ['host' => $host]);
            }

            if ($brand = pbguard_domain::impersonated_brand($host)) {
                $danger[] = $this->t('r_brandlink', ['brand' => $brand, 'host' => $host]);
            }

            if ($like = pbguard_domain::lookalike_of($host, $this->own)) {
                $danger[] = $this->t('r_lookalikelink', ['host' => $host, 'own' => $like]);
            }

            if (pbguard_domain::host_in($host, pbguard_domain::SHORTENERS)) {
                $warn[] = $this->t('r_shortener', ['host' => $host]);
            }
        }

        $shown = self::text_host($text);
        if ($shown && pbguard_domain::registrable($shown) !== $reg && !pbguard_domain::host_in($host, pbguard_domain::TRACKERS)) {
            $reason = $this->t('r_mismatch', ['shown' => $shown, 'host' => $host]);
            // Verified senders linking through their own (or a tracking) domain are
            // usually newsletters, so that case is only a warning.
            if ($this->verified) {
                $warn[] = $reason;
            } else {
                $danger[] = $reason;
            }
        }

        if ($danger) {
            $out['level'] = 'danger';
        } elseif ($warn) {
            $out['level'] = 'warn';
        }
        $out['reasons'] = array_merge($danger, $warn);

        return $out;
    }

    /**
     * Analyse a message.
     *
     * @param array $h     ['from' => raw From, 'replyto' => raw Reply-To, 'subject' => decoded subject,
     *                     'auth' => Authentication-Results (string|array), 'spamd' => X-Spamd-Result,
     *                     'spamstatus' => X-Spam-Status, 'listid' => List-Id, 'charset' => string]
     * @param array $bodies list of ['body' => string, 'html' => bool]
     *
     * @return array ['level', 'verified', 'reasons', 'auth', 'title']
     */
    public function analyze(array $h, array $bodies)
    {
        $danger = [];
        $warn = [];
        $charset = $h['charset'] ?? null;

        // --- sender -------------------------------------------------------------------
        $from = rcube_mime::decode_address_list((string) ($h['from'] ?? ''), 1, true, $charset);
        $from = $from ? reset($from) : [];
        $from_addr = strtolower(trim($from['mailto'] ?? ''));
        $from_name = trim((string) ($from['name'] ?? ''));
        $from_domain = ($p = strrpos($from_addr, '@')) !== false ? pbguard_domain::normalize(substr($from_addr, $p + 1)) : '';
        $this->from_reg = $from_reg = pbguard_domain::registrable($from_domain);

        $auth = self::parse_auth($h['auth'] ?? null);
        $this->verified = $auth['dmarc'] === 'pass' && ($auth['dkim'] === 'pass' || $auth['spf'] === 'pass');

        if ($from_domain) {
            if ($brand = pbguard_domain::impersonated_brand($from_domain)) {
                $danger[] = $this->t('r_brandsender', ['brand' => $brand, 'domain' => $from_domain]);
            }
            if ($like = pbguard_domain::lookalike_of($from_domain, $this->own)) {
                $danger[] = $this->t('r_lookalikesender', ['domain' => $from_domain, 'own' => $like]);
            }
            if (preg_match('/(^|\.)xn--/', $from_domain)) {
                $danger[] = $this->t('r_punycodesender', ['domain' => $from_domain]);
            }
        }

        if ($from_name !== '') {
            if (preg_match('/[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}/i', $from_name, $m) && strtolower($m[0]) !== $from_addr) {
                $danger[] = $this->t('r_nameaddress', ['shown' => strtolower($m[0]), 'real' => $from_addr]);
            } elseif (($brand = pbguard_domain::brand_in_name($from_name)) && $from_reg && !pbguard_domain::brand_owns($brand, $from_reg)
                && !in_array($from_reg, $this->own)
            ) {
                $danger[] = $this->t('r_brandname', ['brand' => $brand, 'domain' => $from_domain]);
            }
        }

        // --- Reply-To -------------------------------------------------------------------
        if (!empty($h['replyto']) && empty($h['listid'])) {
            $rt = rcube_mime::decode_address_list((string) $h['replyto'], 1, true, $charset);
            $rt = $rt ? reset($rt) : [];
            $rt_addr = strtolower($rt['mailto'] ?? '');
            if (($p = strrpos($rt_addr, '@')) !== false) {
                $rt_reg = pbguard_domain::registrable(substr($rt_addr, $p + 1));
                if ($from_reg && $rt_reg && $rt_reg !== $from_reg) {
                    $warn[] = $this->t('r_replyto', ['domain' => $rt_reg, 'from' => $from_reg]);
                }
            }
        }

        // --- links ----------------------------------------------------------------------
        $link_danger = false;
        $link_issue = false;
        $external = false;
        $text = '';
        $seen = [];
        foreach ($bodies as $b) {
            foreach (self::extract_links($b['body'], $b['html']) as $link) {
                $key = $link['href'] . "\0" . $link['text'];
                if (isset($seen[$key])) {
                    continue;
                }
                $seen[$key] = true;

                $r = $this->link_risk($link['href'], $link['text']);
                if ($r['host'] !== '') {
                    $lreg = pbguard_domain::registrable($r['host']);
                    if ($lreg !== $from_reg && !in_array($lreg, $this->own)) {
                        $external = true;
                    }
                }
                if ($r['level'] == 'danger') {
                    $link_danger = $link_issue = true;
                    $danger = array_merge($danger, $r['reasons']);
                } elseif ($r['level'] == 'warn') {
                    $link_issue = true;
                    $warn = array_merge($warn, $r['reasons']);
                }
            }

            if (strlen($text) < 20000) {
                $text .= ' ' . ($b['html'] ? self::text_of(preg_replace('/<(style|script)\b.*?<\/\1>/is', '', $b['body'])) : $b['body']);
            }
        }

        // --- authentication ---------------------------------------------------------------
        if ($auth['dmarc'] === 'fail' || ($auth['spf'] === 'fail' && $auth['dkim'] !== 'pass')) {
            $reason = $auth['dmarc'] === 'fail' ? $this->t('r_dmarcfail', ['domain' => $from_domain]) : $this->t('r_spffail', ['domain' => $from_domain]);
            if ($link_issue) {
                $danger[] = $reason;
            } else {
                $warn[] = $reason;
            }
        }

        $spamd = is_array($h['spamd'] ?? null) ? implode(' ', $h['spamd']) : (string) ($h['spamd'] ?? '');
        if ($spamd !== '' && preg_match_all(self::RSPAMD_DANGER, $spamd, $m)) {
            $danger[] = $this->t('r_rspamd', ['symbols' => implode(', ', array_unique($m[1]))]);
        }

        $status = is_array($h['spamstatus'] ?? null) ? reset($h['spamstatus']) : (string) ($h['spamstatus'] ?? '');
        if (preg_match('/^\s*yes\b/i', $status)) {
            $warn[] = $this->t('r_spam');
        }

        // --- wording ----------------------------------------------------------------------
        if ($external) {
            $hay = mb_strtolower(($h['subject'] ?? '') . ' ' . mb_substr($text, 0, 20000));
            foreach (self::LURES as $phrase) {
                if (mb_strpos($hay, $phrase) !== false) {
                    $warn[] = $this->t('r_lure', ['phrase' => $phrase]);
                    break;
                }
            }
        }

        $danger = array_values(array_unique($danger));
        $warn = array_values(array_unique($warn));
        $level = $danger ? 'danger' : ($warn ? 'warn' : 'none');

        return [
            'level' => $level,
            'verified' => $this->verified && $level != 'danger',
            'reasons' => array_slice(array_merge($danger, $warn), 0, 8),
            'auth' => $auth,
            'title' => $level == 'none' ? '' : $this->t($level == 'danger' ? 'title_danger' : 'title_warn'),
            'links' => $link_danger,
        ];
    }

    private function t($key, $vars = [])
    {
        return call_user_func($this->t, $key, $vars);
    }
}
