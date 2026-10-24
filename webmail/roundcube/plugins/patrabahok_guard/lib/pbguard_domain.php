<?php

/**
 * patrabahok_guard: domain helpers (registrable domain, brand table, lookalikes).
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
class pbguard_domain
{
    /** Second-level labels that sit under a country TLD (example.com.bd, example.co.uk). */
    const SLDS = [
        'co', 'com', 'net', 'org', 'edu', 'gov', 'ac', 'mil', 'gob', 'gouv', 'go', 'or', 'ne',
        'nic', 'sch', 'ltd', 'plc', 'info', 'biz', 'nom', 'int', 'res', 'web', 'ind', 'firm',
        'gen', 'me', 'tv', 'ad', 'gr', 'lg', 'ed', 'mod', 'police', 'nhs', 'sci',
    ];

    /** Multi-label public suffixes that are not "SLD under a ccTLD" (kept small on purpose). */
    const SUFFIXES = [
        'github.io', 'gitlab.io', 'herokuapp.com', 'blogspot.com', 'appspot.com', 'web.app',
        'firebaseapp.com', 'netlify.app', 'vercel.app', 'pages.dev', 'workers.dev', 'azurewebsites.net',
        'cloudfront.net', 'amazonaws.com', 'wordpress.com', 'wixsite.com', 'weebly.com', 'ngrok.io',
        'ngrok-free.app', 'glitch.me', 'repl.co', 'onrender.com', 'fly.dev', 'r2.dev', 'myshopify.com',
        'sharepoint.com', 'translate.goog', 'square.site', 'webflow.io', 'framer.website', 'godaddysites.com',
    ];

    /** URL shorteners: links through them hide the destination. */
    const SHORTENERS = [
        'bit.ly', 'bitly.com', 'tinyurl.com', 't.co', 'goo.gl', 'is.gd', 'cutt.ly', 'rb.gy', 'ow.ly',
        'buff.ly', 'tiny.cc', 'shorturl.at', 'rebrand.ly', 'bl.ink', 't.ly', 'v.gd', 'qrco.de',
        's.id', 'shorte.st', 'adf.ly', 'lnkd.in', 'trib.al', 'soo.gd', 'clck.ru', 'u.to', 'x.gd',
        'shorturl.asia', 'tiny.one', 'urlz.fr', 'surl.li', '0rz.tw', 'bom.so', 'snip.ly', 'linktr.ee',
    ];

    /**
     * Known click-tracking redirectors of mail service providers. Newsletter links
     * routinely show "www.shop.com" while pointing here, so text/href mismatch on these
     * hosts is not treated as phishing.
     */
    const TRACKERS = [
        'list-manage.com', 'mailchimp.com', 'mcusercontent.com', 'sendgrid.net', 'sendgrid.com',
        'mandrillapp.com', 'mailgun.org', 'mailgun.net', 'sparkpostmail.com', 'hubspotlinks.com',
        'hubspotemail.net', 'hs-sites.com', 'klaviyo.com', 'klclick.com', 'klclick1.com', 'klclick2.com',
        'klclick3.com', 'sendibt3.com', 'sendibt2.com', 'brevo.com', 'sendinblue.com', 'r.mailjet.com',
        'mjt.lu', 'createsend.com', 'cmail19.com', 'cmail20.com', 'rs6.net', 'constantcontact.com',
        'exacttarget.com', 'salesforce.com', 'pardot.com', 'mkt.com', 'mktoweb.com', 'awstrack.me',
        'convertkit-mail.com', 'ck.page', 'substack.com', 'beehiiv.com', 'mailerlite.com', 'mlsend.com',
        'safelinks.protection.outlook.com', 'urldefense.com', 'urldefense.proofpoint.com', 'mailtrack.io', 'emltrk.com', 'campaign-archive.com', 'eventbrite.com',
    ];

    /**
     * Brands that phishing mail imitates. 'tokens' match host labels / display names,
     * 'domains' are the brand's own registrable domains. A domain written as "amazon.*"
     * also accepts that name under any country TLD (amazon.co.uk, google.com.bd).
     */
    const BRANDS = [
        'PayPal' => ['tokens' => ['paypal'], 'domains' => ['paypal.com', 'paypal.me', 'paypalobjects.com', 'paypal-community.com', 'paypal.*']],
        'Apple' => ['tokens' => ['apple', 'icloud', 'itunes', 'appleid'], 'domains' => ['apple.com', 'icloud.com', 'me.com', 'mac.com', 'itunes.com', 'mzstatic.com', 'apple.news', 'apple.*']],
        'Microsoft' => ['tokens' => ['microsoft', 'office365', 'outlook', 'onedrive', 'hotmail', 'microsoftonline', 'msoffice', 'sharepoint'],
            'domains' => ['microsoft.com', 'live.com', 'office.com', 'office365.com', 'outlook.com', 'microsoftonline.com', 'msn.com',
                'sharepoint.com', 'onmicrosoft.com', 'azure.com', 'windows.net', 'hotmail.com', 'bing.com', 'skype.com', 'xbox.com',
                'aka.ms', 'microsoft365.com', 'onedrive.com', 'office.net', 'msft.net', 'microsoft.*', 'outlook.*', 'hotmail.*', 'live.*']],
        'Google' => ['tokens' => ['google', 'gmail', 'googledocs', 'googledrive'], 'domains' => ['google.com', 'gmail.com', 'googlemail.com', 'youtube.com',
            'googleusercontent.com', 'gstatic.com', 'goo.gl', 'googleapis.com', 'withgoogle.com', 'g.co', 'google.*', 'blogger.com', 'googlegroups.com']],
        'Amazon' => ['tokens' => ['amazon', 'amazonprime'], 'domains' => ['amazon.com', 'amazonaws.com', 'amazonses.com', 'a.co', 'primevideo.com', 'amazon.*', 'awstrack.me']],
        'Netflix' => ['tokens' => ['netflix'], 'domains' => ['netflix.com', 'netflix.net', 'nflxext.com', 'nflximg.net', 'netflix.*']],
        'Facebook' => ['tokens' => ['facebook', 'facebookmail'], 'domains' => ['facebook.com', 'facebookmail.com', 'fb.com', 'fb.me', 'fbcdn.net', 'meta.com', 'messenger.com', 'metamail.com']],
        'Instagram' => ['tokens' => ['instagram'], 'domains' => ['instagram.com', 'cdninstagram.com', 'facebookmail.com', 'mail.instagram.com', 'meta.com']],
        'WhatsApp' => ['tokens' => ['whatsapp'], 'domains' => ['whatsapp.com', 'whatsapp.net', 'wa.me', 'facebookmail.com', 'meta.com']],
        'LinkedIn' => ['tokens' => ['linkedin'], 'domains' => ['linkedin.com', 'licdn.com', 'lnkd.in']],
        'DHL' => ['tokens' => ['dhl'], 'domains' => ['dhl.com', 'dhl.de', 'dhl.*', 'dhlparcel.nl', 'dhl-news.com', 'deutschepost.de']],
        'FedEx' => ['tokens' => ['fedex'], 'domains' => ['fedex.com', 'fedex.*']],
        'UPS' => ['tokens' => ['upsdelivery', 'ups-delivery'], 'domains' => ['ups.com']],
        'DocuSign' => ['tokens' => ['docusign'], 'domains' => ['docusign.com', 'docusign.net']],
        'Dropbox' => ['tokens' => ['dropbox'], 'domains' => ['dropbox.com', 'dropboxmail.com', 'dropboxusercontent.com']],
        'Adobe' => ['tokens' => ['adobe'], 'domains' => ['adobe.com', 'adobesign.com', 'adobelogin.com', 'adobe.*']],
        'WeTransfer' => ['tokens' => ['wetransfer'], 'domains' => ['wetransfer.com', 'we.tl']],
        'Coinbase' => ['tokens' => ['coinbase'], 'domains' => ['coinbase.com']],
        'Binance' => ['tokens' => ['binance'], 'domains' => ['binance.com']],
        'bKash' => ['tokens' => ['bkash'], 'domains' => ['bkash.com', 'bka.sh']],
        'Nagad' => ['tokens' => ['nagad'], 'domains' => ['nagad.com.bd']],
        'Rocket' => ['tokens' => ['dbblrocket', 'rocketbd'], 'domains' => ['dutchbanglabank.com', 'dbbl.com.bd']],
        'Upay' => ['tokens' => ['upaybd'], 'domains' => ['upaybd.com']],
        'Dutch-Bangla Bank' => ['tokens' => ['dutchbangla', 'dbbl'], 'domains' => ['dutchbanglabank.com', 'dbbl.com.bd']],
        'Sonali Bank' => ['tokens' => ['sonalibank'], 'domains' => ['sonalibank.com.bd']],
        'BRAC Bank' => ['tokens' => ['bracbank'], 'domains' => ['bracbank.com']],
        'City Bank' => ['tokens' => ['thecitybank', 'citybankbd'], 'domains' => ['thecitybank.com']],
        'Islami Bank' => ['tokens' => ['islamibank'], 'domains' => ['islamibankbd.com']],
        'Prime Bank' => ['tokens' => ['primebank'], 'domains' => ['primebank.com.bd']],
        'Eastern Bank' => ['tokens' => ['easternbank'], 'domains' => ['ebl.com.bd']],
        'Janata Bank' => ['tokens' => ['janatabank'], 'domains' => ['jb.com.bd']],
        'Agrani Bank' => ['tokens' => ['agranibank'], 'domains' => ['agranibank.org']],
        'Standard Chartered' => ['tokens' => ['standardchartered'], 'domains' => ['sc.com', 'standardchartered.com']],
        'HSBC' => ['tokens' => ['hsbc'], 'domains' => ['hsbc.com', 'hsbc.com.bd', 'hsbc.co.uk', 'hsbc.*']],
        'Chase' => ['tokens' => ['chase', 'chaseonline'], 'domains' => ['chase.com', 'jpmorganchase.com', 'jpmorgan.com']],
        'Wells Fargo' => ['tokens' => ['wellsfargo'], 'domains' => ['wellsfargo.com']],
        'Bank of America' => ['tokens' => ['bankofamerica'], 'domains' => ['bankofamerica.com', 'bofa.com']],
        'Citibank' => ['tokens' => ['citibank'], 'domains' => ['citi.com', 'citibank.com']],
    ];

    /** Display-name words per brand (whole words, lowercase). */
    const BRAND_NAMES = [
        'PayPal' => ['paypal'], 'Apple' => ['apple', 'icloud', 'apple id', 'itunes'],
        'Microsoft' => ['microsoft', 'office 365', 'office365', 'outlook', 'onedrive', 'microsoft 365', 'sharepoint'],
        'Google' => ['google', 'gmail'], 'Amazon' => ['amazon'], 'Netflix' => ['netflix'], 'Facebook' => ['facebook'],
        'Instagram' => ['instagram'], 'WhatsApp' => ['whatsapp'], 'LinkedIn' => ['linkedin'], 'DHL' => ['dhl'],
        'FedEx' => ['fedex'], 'DocuSign' => ['docusign'], 'Dropbox' => ['dropbox'], 'WeTransfer' => ['wetransfer'],
        'Coinbase' => ['coinbase'], 'Binance' => ['binance'], 'bKash' => ['bkash'], 'Nagad' => ['nagad'],
        'Dutch-Bangla Bank' => ['dutch-bangla', 'dutch bangla', 'dbbl'], 'Sonali Bank' => ['sonali bank'],
        'BRAC Bank' => ['brac bank'], 'Islami Bank' => ['islami bank'], 'Prime Bank' => ['prime bank'],
        'HSBC' => ['hsbc'], 'Chase' => ['chase bank'], 'Wells Fargo' => ['wells fargo'], 'Bank of America' => ['bank of america'],
    ];

    /**
     * Lowercase, trim dots, and convert a Unicode host to its ASCII (punycode) form.
     */
    public static function normalize($host)
    {
        $host = strtolower(trim((string) $host, " \t\r\n.[]"));
        if ($host === '') {
            return '';
        }

        if (preg_match('/[^\x20-\x7e]/', $host) && function_exists('idn_to_ascii')) {
            $ascii = @idn_to_ascii($host, IDNA_DEFAULT, INTL_IDNA_VARIANT_UTS46);
            if ($ascii) {
                return strtolower($ascii);
            }
        }

        return $host;
    }

    public static function is_ip($host)
    {
        $host = trim($host, '[]');
        if (filter_var($host, FILTER_VALIDATE_IP)) {
            return true;
        }

        // Decimal / hex / octal encodings (http://3232235777/, http://0xC0A80001/).
        return (bool) preg_match('/^(0x[0-9a-f]+|[0-9]+)(\.(0x[0-9a-f]+|[0-9]+)){0,3}$/i', $host);
    }

    /**
     * Approximate registrable domain without a full public suffix list: the last two
     * labels, or three when the second-to-last label is a known SLD under a ccTLD
     * (example.com.bd, example.co.uk), or a known multi-label suffix (x.github.io).
     */
    public static function registrable($host)
    {
        $host = self::normalize($host);
        if ($host === '' || self::is_ip($host)) {
            return $host;
        }

        $labels = explode('.', $host);
        $n = count($labels);
        if ($n <= 2) {
            return $host;
        }

        $last2 = $labels[$n - 2] . '.' . $labels[$n - 1];
        if (in_array($last2, self::SUFFIXES) && $n >= 3) {
            return $labels[$n - 3] . '.' . $last2;
        }

        if (strlen($labels[$n - 1]) == 2 && in_array($labels[$n - 2], self::SLDS)) {
            return $labels[$n - 3] . '.' . $last2;
        }

        return $last2;
    }

    /**
     * Is $registrable one of the given domains? "name.*" accepts the name under any
     * country TLD (with or without a known SLD): amazon.co.uk, google.com.bd, dhl.de.
     */
    public static function in_domains($registrable, $domains)
    {
        foreach ($domains as $d) {
            if (substr($d, -2) === '.*') {
                $name = substr($d, 0, -2);
                if (preg_match('/^' . preg_quote($name, '/') . '\.(?:(?:' . implode('|', self::SLDS) . ')\.)?[a-z]{2}$/', $registrable)) {
                    return true;
                }
            } elseif ($registrable === $d) {
                return true;
            }
        }

        return false;
    }

    /** Is $host the domain or any subdomain of one of $list? */
    public static function host_in($host, $list)
    {
        foreach ($list as $d) {
            if ($host === $d || substr($host, -strlen($d) - 1) === '.' . $d) {
                return true;
            }
        }

        return false;
    }

    /**
     * The brand a host imitates, or null. A host label (or its hyphen-separated parts,
     * or the label without hyphens) must equal a brand token or start with it; the
     * host's registrable domain must not be one of the brand's own.
     */
    public static function impersonated_brand($host)
    {
        $host = self::normalize($host);
        if ($host === '' || self::is_ip($host)) {
            return null;
        }

        $reg = self::registrable($host);
        $parts = [];
        foreach (explode('.', $host) as $label) {
            $parts[] = $label;
            $parts[] = str_replace('-', '', $label);
            foreach (explode('-', $label) as $p) {
                $parts[] = $p;
            }
        }
        $parts = array_unique(array_filter($parts));

        foreach (self::BRANDS as $brand => $def) {
            foreach ($def['tokens'] as $token) {
                foreach ($parts as $p) {
                    if (self::part_matches($p, $token)) {
                        if (!self::in_domains($reg, $def['domains'])) {
                            return $brand;
                        }

                        continue 3;
                    }
                }
            }
        }

        return null;
    }

    /** Words that typically follow a brand name in a phishing hostname (paypalsecure, appleid). */
    const LURE_SUFFIX = '/^(\d|s$|id|log|sign|secur|support|account|verif|servic|online|updat|help|bill|auth|team|cent|pay|mail|web|app|bank|net|user|alert|info|bd$|us$|uk$|official|customer|care|portal|doc|drive|store|cloud|wallet|confirm|recover|unlock|limit|resolution|notice)/';

    private static function part_matches($part, $token)
    {
        if ($part === $token) {
            return true;
        }

        if (strpos($part, $token) === 0 && preg_match(self::LURE_SUFFIX, substr($part, strlen($token)))) {
            return true;
        }

        // securepaypal, my-netflix: long tokens at the end of a label
        return strlen($token) >= 6 && substr($part, -strlen($token)) === $token;
    }

    /** The brand a display name claims to be (whole-word match), or null. */
    public static function brand_in_name($name)
    {
        $name = ' ' . preg_replace('/[^a-z0-9\-]+/', ' ', mb_strtolower((string) $name)) . ' ';
        foreach (self::BRAND_NAMES as $brand => $words) {
            foreach ($words as $w) {
                if (strpos($name, ' ' . $w . ' ') !== false) {
                    return $brand;
                }
            }
        }

        return null;
    }

    public static function brand_owns($brand, $registrable)
    {
        return isset(self::BRANDS[$brand]) && self::in_domains($registrable, self::BRANDS[$brand]['domains']);
    }

    /**
     * Does $domain look like one of $own (edit distance 1–2) without being it?
     */
    public static function lookalike_of($domain, $own)
    {
        $domain = self::registrable($domain);
        if (strlen($domain) < 6) {
            return null;
        }

        foreach ($own as $o) {
            if ($o === $domain || strlen($o) < 6) {
                continue;
            }

            if (abs(strlen($o) - strlen($domain)) > 2) {
                continue;
            }

            $d = levenshtein($domain, $o);
            if ($d >= 1 && $d <= 2) {
                return $o;
            }

            // A swapped pair of letters counts as one edit (barailahs vs baraliahs).
            if (strlen($o) == strlen($domain) && self::is_transposition($domain, $o)) {
                return $o;
            }
        }

        return null;
    }

    private static function is_transposition($a, $b)
    {
        $diff = [];
        for ($i = 0, $n = strlen($a); $i < $n; $i++) {
            if ($a[$i] !== $b[$i]) {
                $diff[] = $i;
                if (count($diff) > 4) {
                    return false;
                }
            }
        }

        return count($diff) == 2 && $diff[1] == $diff[0] + 1
            && $a[$diff[0]] === $b[$diff[1]] && $a[$diff[1]] === $b[$diff[0]];
    }

    /**
     * Does a hostname mix scripts inside one label (Latin + Cyrillic, ...)? Works on the
     * Unicode form, so punycode labels are decoded first.
     */
    public static function mixed_script($host)
    {
        $u = $host;
        if (strpos($host, 'xn--') !== false && function_exists('idn_to_utf8')) {
            $u = @idn_to_utf8($host, IDNA_DEFAULT, INTL_IDNA_VARIANT_UTS46) ?: $host;
        }

        if (!preg_match('/[^\x00-\x7f]/', $u)) {
            return false;
        }

        foreach (explode('.', $u) as $label) {
            $latin = preg_match('/[a-z]/i', $label);
            $other = preg_match('/[\p{Cyrillic}\p{Greek}\p{Armenian}\p{Cherokee}]/u', $label);
            if ($latin && $other) {
                return true;
            }
        }

        return false;
    }
}
