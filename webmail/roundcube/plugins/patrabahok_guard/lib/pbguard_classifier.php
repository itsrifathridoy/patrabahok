<?php

/**
 * patrabahok_guard: header-only inbox categorization (primary, social, promotions,
 * updates, forums), Gmail-tab style.
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
class pbguard_classifier
{
    const CATEGORIES = ['primary', 'social', 'promotions', 'updates', 'forums'];

    /** IMAP keyword marking a message as classified; plus one keyword per non-primary category. */
    const MARKER = '$pbcat';

    const KEYWORDS = [
        'social' => '$pbsocial',
        'promotions' => '$pbpromotions',
        'updates' => '$pbupdates',
        'forums' => '$pbforums',
    ];

    /** Headers the classifier needs (fetched with BODY.PEEK[HEADER.FIELDS]). */
    const HEADERS = [
        'LIST-ID', 'LIST-UNSUBSCRIBE', 'LIST-POST', 'PRECEDENCE', 'AUTO-SUBMITTED', 'FEEDBACK-ID', 'X-MAILER',
        'X-CAMPAIGN', 'X-CAMPAIGNID', 'X-MC-USER', 'X-SG-EID', 'X-SES-OUTGOING', 'X-MAILGUN-TAG', 'X-MANDRILL-USER',
        'X-CSA-COMPLAINTS', 'X-MAILMAN-VERSION', 'MAILING-LIST', 'X-GOOGLE-GROUP-ID', 'X-DISCOURSE-POST-ID',
        'X-GITHUB-REASON', 'X-GITLAB-PROJECT', 'X-JIRA-FINGERPRINT', 'X-ATLASSIAN-TOKEN', 'X-AUTO-RESPONSE-SUPPRESS',
        'X-AUTOREPLY', 'X-LINKEDIN-CLASS', 'X-FACEBOOK-NOTIFY', 'X-BULK', 'X-MARKETO-ID', 'X-HUBSPOT-ID',
    ];

    const SOCIAL = [
        'facebookmail.com', 'facebook.com', 'linkedin.com', 'twitter.com', 'x.com', 'instagram.com', 'tiktok.com',
        'pinterest.com', 'reddit.com', 'redditmail.com', 'discord.com', 'discordapp.com', 'quora.com', 'youtube.com',
        'snapchat.com', 'tumblr.com', 'threads.net', 'telegram.org', 'vk.com', 'nextdoor.com', 'meetup.com',
        'strava.com', 'goodreads.com', 'flickr.com', 'mastodon.social', 'bsky.app', 'twitch.tv', 'whatsapp.com',
        'messenger.com', 'skype.com', 'viber.com', 'imo.im', 'likee.video', 'researchgate.net', 'academia.edu',
    ];

    const DEVTOOLS = ['github.com', 'gitlab.com', 'atlassian.net', 'atlassian.com', 'bitbucket.org', 'jira.com', 'sentry.io', 'vercel.com', 'netlify.com'];

    const ESP_MAILERS = '/mailchimp|sendgrid|hubspot|klaviyo|sendinblue|brevo|constant ?contact|campaign ?monitor|mailjet|acoustic|exacttarget|marketo|pardot|mailerlite|convertkit|activecampaign|omnisend|moosend|getresponse|aweber|mailpoet|sendy|substack|beehiiv/i';

    const MARKETING = '/\b(sale|sales|deals?|offers?|discount|coupon|promo(code)?|save|savings|off|newsletter|webinar|free shipping|limited time|shop now|buy now|new arrivals?|black friday|cyber monday|exclusive|clearance|subscribe|special|bonus|gift|% ?off|\d+%|last chance|don\'t miss|early bird|flash|launch|introducing|announcing|upgrade now|trial|join us|register now|event|cashback|reward)\b|%|৳|ছাড়|অফার/iu';

    const TRANSACTIONAL = '/\b(order|orders|receipt|invoice|payment|paid|statement|shipped|shipping|delivered|delivery|tracking|otp|one[- ]time|code|password|verify|verification|confirm|confirmation|security|alert|sign[- ]?in|login|account|subscription|renewal|renewed|booking|reservation|ticket|itinerary|bill|transaction|refund|reset|expires?|expiring|reminder|notification|report|summary|your request|welcome to|activated?)\b|ওটিপি|পাসওয়ার্ড/iu';

    const AUTOMATED_LOCAL = '/^(no[-_.]?reply|do[-_.]?not[-_.]?reply|donotreply|noreply|notifications?|notify|alerts?|mailer(-daemon)?|automated|auto|bounces?|system|robot|bot|accounts?|security|billing|receipts?|orders?|invoices?|updates?|account-?security|verify|verification|info|support|service|team|hello)([-_.+].*)?$/i';

    /**
     * Classify one message from its header object.
     *
     * @param rcube_message_header $hdr
     * @param string[]             $own registrable domains of the user's own addresses
     */
    public static function classify($hdr, array $own = [])
    {
        $o = is_array($hdr->others ?? null) ? $hdr->others : [];
        $get = function ($name) use ($o) {
            $v = $o[$name] ?? '';
            return is_array($v) ? implode(' ', $v) : (string) $v;
        };

        $from = rcube_mime::decode_address_list((string) $hdr->from, 1, true, $hdr->charset ?? null);
        $from = $from ? reset($from) : [];
        $addr = strtolower($from['mailto'] ?? '');
        $local = ($p = strrpos($addr, '@')) !== false ? substr($addr, 0, $p) : $addr;
        $domain = $p !== false ? substr($addr, $p + 1) : '';
        $reg = pbguard_domain::registrable($domain);
        $subject = rcube_mime::decode_header((string) $hdr->subject, $hdr->charset ?? null);

        $listid = strtolower($get('list-id'));
        $listid_dom = preg_match('/<([^>]+)>/', $listid, $m) ? $m[1] : trim($listid);
        $precedence = strtolower(trim($get('precedence')));
        $auto = strtolower(trim($get('auto-submitted')));
        $mailer = $get('x-mailer');
        $feedback = $get('feedback-id');
        $unsub = $get('list-unsubscribe') !== '';

        // social: the sender (or the list) is a social network
        if (pbguard_domain::host_in($domain, self::SOCIAL) || pbguard_domain::host_in(pbguard_domain::registrable($listid_dom), self::SOCIAL)
            || ($listid_dom && pbguard_domain::host_in($listid_dom, self::SOCIAL))
            || $get('x-linkedin-class') !== '' || $get('x-facebook-notify') !== ''
        ) {
            return 'social';
        }

        // developer tools: notifications are updates, discussions are forums
        if ($get('x-github-reason') !== '' || $get('x-gitlab-project') !== '' || $get('x-jira-fingerprint') !== ''
            || $get('x-atlassian-token') !== '' || pbguard_domain::host_in($domain, self::DEVTOOLS)
        ) {
            return preg_match('/\(Discussion #\d+\)|\bdiscussion\b/i', $subject) ? 'forums' : 'updates';
        }

        $esp = $get('x-sg-eid') !== '' || $get('x-ses-outgoing') !== '' || $get('x-mc-user') !== '' || $get('x-mandrill-user') !== ''
            || $get('x-mailgun-tag') !== '' || $get('x-csa-complaints') !== '' || $get('x-campaign') !== '' || $get('x-campaignid') !== ''
            || $get('x-marketo-id') !== '' || $get('x-hubspot-id') !== '' || ($mailer !== '' && preg_match(self::ESP_MAILERS, $mailer))
            || preg_match('/campaign|newsletter|promo|marketing|mailchimp|mc:/i', $feedback)
            || preg_match('/mcsv\.net|mailchimp|list-manage|createsend|klaviyo|hubspot|sendgrid/i', $listid);
        $bulk = $esp || $unsub || $precedence === 'bulk' || $precedence === 'junk' || $get('x-bulk') !== '';

        // forums: real discussion lists (List-Post, Mailman, Google Groups, Discourse)
        $forum = $get('list-post') !== '' && stripos($get('list-post'), 'NO') !== 0
            || $get('x-mailman-version') !== '' || $get('mailing-list') !== '' || $get('x-google-group-id') !== ''
            || $get('x-discourse-post-id') !== '' || pbguard_domain::host_in($domain, ['googlegroups.com', 'groups.io', 'discoursemail.com'])
            || ($precedence === 'list' && $listid !== '');
        if ($forum && !$esp) {
            return 'forums';
        }

        $automated = ($auto !== '' && $auto !== 'no') || $get('x-auto-response-suppress') !== '' || $get('x-autoreply') !== ''
            || preg_match(self::AUTOMATED_LOCAL, $local);
        $marketing = (bool) preg_match(self::MARKETING, $subject);
        $transactional = (bool) preg_match(self::TRANSACTIONAL, $subject);

        if (($automated || $bulk) && $transactional && !$marketing) {
            return 'updates';
        }

        if ($bulk) {
            if ($marketing || $esp || !$automated) {
                return 'promotions';
            }

            return 'updates';
        }

        // Personal mail (no list / bulk headers), including from the user's own domain.
        if ($automated && !in_array($reg, $own) && preg_match('/^(no[-_.]?reply|do[-_.]?not[-_.]?reply|donotreply|noreply|notifications?|notify|alerts?|mailer|automated)/i', $local)) {
            return 'updates';
        }

        return 'primary';
    }

    /** IMAP SEARCH criteria for one category (usable with rcmail.filter_mailbox()). */
    public static function filter($cat)
    {
        if ($cat == 'primary') {
            $parts = [];
            foreach (self::KEYWORDS as $kw) {
                $parts[] = 'NOT KEYWORD ' . $kw;
            }
            return implode(' ', $parts);
        }

        return 'KEYWORD ' . self::KEYWORDS[$cat];
    }
}
