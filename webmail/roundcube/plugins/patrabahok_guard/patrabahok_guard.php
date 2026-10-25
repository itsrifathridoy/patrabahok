<?php

/**
 * patrabahok_guard: mail safety for the patrabahok Webmail (Roundcube).
 *
 * - Phishing / sender-trust analysis of every displayed message: a warning banner,
 *   marked risky links with a confirmation before they open, and a "verified sender"
 *   flag (DMARC-aligned DKIM/SPF pass from Authentication-Results)
 * - ClamAV (clamd INSTREAM) scanning of attachments: uploads in compose are blocked
 *   when infected, received attachments get a scan badge and infected parts are never
 *   served
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
class patrabahok_guard extends rcube_plugin
{
    public $task = 'mail';

    /** Headers the analysis reads, added to the IMAP header fetch. */
    const FETCH_HEADERS = [
        'AUTHENTICATION-RESULTS', 'X-SPAMD-RESULT', 'X-SPAM-STATUS', 'LIST-UNSUBSCRIBE', 'LIST-ID', 'PRECEDENCE',
        'AUTO-SUBMITTED', 'FEEDBACK-ID', 'X-MAILER',
    ];

    const SCAN_CACHE_MAX = 300;

    /** @var array|null analysis of the message being shown */
    private $result;

    /** @var pbguard_analyzer|null */
    private $analyzer;

    public function init()
    {
        require_once __DIR__ . '/lib/pbguard_domain.php';
        require_once __DIR__ . '/lib/pbguard_analyzer.php';
        require_once __DIR__ . '/lib/pbguard_clamd.php';

        $this->load_config();

        $this->add_hook('storage_init', [$this, 'storage_init']);
        $this->add_hook('startup', [$this, 'startup']);
        $this->add_hook('render_page', [$this, 'render_page']);
        $this->add_hook('message_objects', [$this, 'message_objects']);
        $this->add_hook('message_part_after', [$this, 'message_part_after']);
        $this->add_hook('message_part_get', [$this, 'message_part_get']);
        $this->add_hook('attachment_upload', [$this, 'attachment_upload']);
        $this->add_hook('attachment_save', [$this, 'attachment_upload']);

        $this->register_action('plugin.pbguard-scan', [$this, 'action_scan']);
    }

    // ---------------------------------------------------------------------------------
    // Page setup
    // ---------------------------------------------------------------------------------

    public function storage_init($p)
    {
        $action = rcmail::get_instance()->action;
        if (in_array($action, ['show', 'preview', 'print'])) {
            $have = preg_split('/\s+/', strtoupper(trim($p['fetch_headers'] ?? '')), -1, PREG_SPLIT_NO_EMPTY);
            $p['fetch_headers'] = implode(' ', array_unique(array_merge($have, self::FETCH_HEADERS)));
        }

        return $p;
    }

    public function render_page($args)
    {
        $rcmail = rcmail::get_instance();
        if ($rcmail->output->type != 'html') {
            return $args;
        }

        $template = $args['template'];
        if (!in_array($template, ['mail', 'message', 'messagepreview', 'compose'])) {
            return $args;
        }

        $this->include_stylesheet($this->local_skin_path() . '/guard.css');
        $this->include_script('guard.js');
        $this->add_texts('localization/', [
            'linkdialogtitle', 'linkdialogtext', 'linkdialogreal', 'goback', 'openanyway', 'dismiss',
            'scanpending', 'scanclean', 'scaninfected', 'scanerror', 'scanskipped', 'scanpendingtitle',
            'scancleantitle', 'scaninfectedtitle', 'scanerrortitle', 'scanskippedtitle', 'attblocked', 'verifiedsender',
        ]);

        return $args;
    }

    // ---------------------------------------------------------------------------------
    // Phishing / sender trust
    // ---------------------------------------------------------------------------------

    /**
     * Banner above the message body (Roundcube's messageObjects container, which the
     * skin places right above the body). Runs before the body is rendered, so the
     * analysis is done here, from the raw parts; Roundcube keeps the fetched bodies in
     * memory and reuses them for display.
     */
    public function message_objects($args)
    {
        $message = $args['message'] ?? null;
        if (!$message || empty($message->headers)) {
            return $args;
        }

        $rcmail = rcmail::get_instance();
        $res = $this->analyze_message($message);

        $rcmail->output->set_env('pb_guard', [
            'level' => $res['level'],
            'verified' => $res['verified'],
            'reasons' => $res['reasons'],
            'auth' => $res['auth'],
        ]);

        if ($this->scan_enabled()) {
            $rcmail->output->set_env('pb_scan', true);
        }

        if ($res['level'] != 'none') {
            $items = '';
            foreach ($res['reasons'] as $r) {
                $items .= html::tag('li', null, rcube::Q($r));
            }

            $banner = html::div(['class' => 'pb-guard-banner pb-guard-' . $res['level'], 'role' => 'alert'],
                html::span(['class' => 'pb-guard-icon', 'aria-hidden' => 'true'], '')
                . html::div('pb-guard-text', html::tag('strong', null, rcube::Q($res['title'])) . html::tag('ul', null, $items))
                . html::tag('button', ['type' => 'button', 'class' => 'pb-guard-dismiss', 'title' => $this->gettext('dismiss'),
                    'aria-label' => $this->gettext('dismiss')], '&times;')
            );

            array_unshift($args['content'], $banner);
        }

        return $args;
    }

    /** Mark risky links in the washed (safe) HTML or the linkified plain text. */
    public function message_part_after($args)
    {
        $rcmail = rcmail::get_instance();
        if (!in_array($rcmail->action, ['show', 'preview', 'print', 'get']) || !in_array($args['type'], ['html', 'plain'])
            || stripos($args['body'], '<a') === false
        ) {
            return $args;
        }

        $analyzer = $this->analyzer();
        $args['body'] = preg_replace_callback('/<a\b([^>]*)>(.*?)<\/a\s*>/is', function ($m) use ($analyzer) {
            $href = pbguard_analyzer::attr($m[1], 'href');
            if ($href === null) {
                return $m[0];
            }

            $risk = $analyzer->link_risk($href, pbguard_analyzer::text_of($m[2]));
            if ($risk['level'] == 'none') {
                return $m[0];
            }

            $attrs = $m[1];
            if (preg_match('/\sclass\s*=\s*"([^"]*)"/i', $attrs)) {
                $attrs = preg_replace('/(\sclass\s*=\s*")([^"]*)"/i', '$1$2 pb-link-risk"', $attrs, 1);
            } else {
                $attrs .= ' class="pb-link-risk"';
            }
            $attrs = preg_replace('/\sdata-pb-(risk|real)\s*=\s*("[^"]*"|\'[^\']*\'|[^\s>]+)/i', '', $attrs);
            $attrs .= ' data-pb-risk="' . $risk['level'] . '" data-pb-real="' . rcube::Q($risk['host']) . '"';

            return '<a' . $attrs . '>' . $m[2] . '</a>';
        }, $args['body']);

        return $args;
    }

    private function analyzer()
    {
        if (!$this->analyzer) {
            $this->analyzer = new pbguard_analyzer(function ($key, $vars) {
                return $this->gettext(['name' => $key, 'vars' => $vars]);
            }, $this->own_domains());
        }

        return $this->analyzer;
    }

    private function analyze_message($message)
    {
        if ($this->result !== null) {
            return $this->result;
        }

        $this->add_texts('localization/');
        $h = $message->headers;
        $o = is_array($h->others ?? null) ? $h->others : [];

        $bodies = [];
        $budget = 2 * 1024 * 1024;
        $parts = !empty($message->parts) ? $message->parts : [];
        foreach ($parts as $part) {
            if ($part->type != 'content' || $part->ctype_primary != 'text' || !in_array($part->ctype_secondary, ['html', 'plain'])
                || !$part->size || $part->size > $budget
            ) {
                continue;
            }
            $body = $message->get_part_body($part->mime_id, true);
            if (is_string($body)) {
                $budget -= strlen($body);
                $bodies[] = ['body' => $body, 'html' => $part->ctype_secondary == 'html'];
            }
        }
        if (!$parts && isset($message->body) && is_string($message->body)) {
            $bodies[] = ['body' => $message->body, 'html' => false];
        }

        $analyzer = $this->analyzer();
        $this->result = $analyzer->analyze([
            'from' => $h->from,
            'replyto' => $h->replyto ?? '',
            'subject' => $message->subject ?? rcube_mime::decode_header($h->subject, $h->charset),
            'auth' => $o['authentication-results'] ?? null,
            'spamd' => $o['x-spamd-result'] ?? null,
            'spamstatus' => $o['x-spam-status'] ?? null,
            'listid' => $o['list-id'] ?? null,
            'charset' => $h->charset,
        ], $bodies);

        return $this->result;
    }

    /** Registrable domains of the user's login and identities (cached in the session). */
    private function own_domains()
    {
        if (isset($_SESSION['pbguard_own']) && is_array($_SESSION['pbguard_own'])) {
            return $_SESSION['pbguard_own'];
        }

        $rcmail = rcmail::get_instance();
        $emails = [$rcmail->get_user_email(), $rcmail->get_user_name()];
        if ($rcmail->user && ($list = $rcmail->user->list_emails())) {
            foreach ($list as $i) {
                $emails[] = $i['email'];
            }
        }

        $domains = [];
        foreach ($emails as $e) {
            if (is_string($e) && ($p = strrpos($e, '@')) !== false) {
                $domains[] = pbguard_domain::registrable(substr($e, $p + 1));
            }
        }

        return $_SESSION['pbguard_own'] = array_values(array_unique(array_filter($domains)));
    }

    // ---------------------------------------------------------------------------------
    // ClamAV
    // ---------------------------------------------------------------------------------

    private function clamd()
    {
        $rcmail = rcmail::get_instance();

        return new pbguard_clamd(
            (string) $rcmail->config->get('patrabahok_guard_clamd', 'unix:///var/run/clamav/clamd.ctl'),
            (float) $rcmail->config->get('patrabahok_guard_clamd_connect_timeout', 2),
            (float) $rcmail->config->get('patrabahok_guard_clamd_scan_timeout', 20)
        );
    }

    private function scan_enabled()
    {
        return $this->clamd()->available();
    }

    private function scan_max()
    {
        return (int) rcmail::get_instance()->config->get('patrabahok_guard_scan_max_size', 25 * 1024 * 1024);
    }

    /** Run a scan, remembering for a minute that clamd is down so the UI never stalls. */
    private function scan($source, $is_data = false)
    {
        if (!empty($_SESSION['pbguard_clamd_down']) && $_SESSION['pbguard_clamd_down'] > time() - 60) {
            return ['status' => 'error', 'virus' => null];
        }

        $clamd = $this->clamd();
        $res = $clamd->scan($source, $is_data);
        if ($res['status'] == 'error') {
            rcube::write_log('errors', 'patrabahok_guard: clamd scan failed: ' . $clamd->last_error);
            if (preg_match('/^connect failed/', $clamd->last_error)) {
                $_SESSION['pbguard_clamd_down'] = time();
            }
        } else {
            unset($_SESSION['pbguard_clamd_down']);
        }

        return $res;
    }

    /** Compose upload (and attachments saved from drafts / forwards). */
    public function attachment_upload($args)
    {
        if (!empty($args['abort']) || !$this->scan_enabled()) {
            return $args;
        }

        $path = $args['path'] ?? null;
        $size = $path && is_file($path) ? filesize($path) : strlen((string) ($args['data'] ?? ''));
        if ((!$path && !isset($args['data'])) || $size > $this->scan_max()) {
            return $args;
        }

        $res = $path ? $this->scan($path) : $this->scan($args['data'], true);
        if ($res['status'] == 'infected') {
            $this->add_texts('localization/');
            if ($path) {
                @unlink($path);
            }
            rcube::write_log('errors', sprintf('patrabahok_guard: blocked upload "%s" (%s) for %s',
                $args['name'] ?? '', $res['virus'], rcmail::get_instance()->get_user_name()));

            $args['abort'] = true;
            $args['status'] = false;
            $args['break'] = true;
            $args['error'] = $this->gettext(['name' => 'uploadblocked', 'vars' => ['virus' => $res['virus']]]);
            unset($args['path'], $args['data']);
        }

        return $args;
    }

    /** Scan one message part and cache the result. */
    private function scan_part($mbox, $uid, $part, $message = null)
    {
        $key = $this->scan_key($mbox, $uid, $part->mime_id);
        if (isset($_SESSION['pbguard_scan'][$key]) && $_SESSION['pbguard_scan'][$key]['status'] != 'error') {
            return $_SESSION['pbguard_scan'][$key];
        }

        $name = rcmail_action_mail_index::attachment_name($part);
        if ($part->size > $this->scan_max()) {
            return ['status' => 'skipped', 'virus' => null, 'name' => $name];
        }

        $rcmail = rcmail::get_instance();
        $fp = fopen('php://temp/maxmemory:1048576', 'w+');
        if ($message) {
            $ok = $message->get_part_body($part->mime_id, false, 0, $fp);
        } else {
            $rcmail->storage->set_folder($mbox);
            $ok = $rcmail->storage->get_message_part($uid, $part->mime_id, $part, null, $fp, true, 0, false);
        }

        if ($ok === false) {
            fclose($fp);
            return ['status' => 'error', 'virus' => null, 'name' => $name];
        }

        rewind($fp);
        $res = $this->scan($fp);
        fclose($fp);

        $out = ['status' => $res['status'], 'virus' => $res['virus'], 'name' => $name];
        if ($res['status'] != 'error') {
            $_SESSION['pbguard_scan'][$key] = $out;
            if (count($_SESSION['pbguard_scan']) > self::SCAN_CACHE_MAX) {
                $_SESSION['pbguard_scan'] = array_slice($_SESSION['pbguard_scan'], -self::SCAN_CACHE_MAX, null, true);
            }
        }

        return $out;
    }

    private function scan_key($mbox, $uid, $mime_id)
    {
        return md5($mbox) . ':' . $uid . ':' . $mime_id;
    }

    /** AJAX: scan every attachment of one message. */
    public function action_scan()
    {
        $rcmail = rcmail::get_instance();
        $uid = rcube_utils::get_input_string('_uid', rcube_utils::INPUT_GPC);
        $mbox = rcube_utils::get_input_string('_mbox', rcube_utils::INPUT_GPC, true);

        if (!preg_match('/^[1-9][0-9]{0,9}$/', $uid) || !strlen($mbox) || !$rcmail->storage->folder_exists($mbox)) {
            $rcmail->output->command('plugin.pbguard_scan_result', ['uid' => $uid, 'parts' => [], 'error' => 'invalid']);
            $rcmail->output->send();
        }

        $parts = [];
        if ($this->scan_enabled()) {
            $message = new rcube_message($uid, $mbox);
            foreach ((array) $message->attachments as $part) {
                $parts[$part->mime_id] = $this->scan_part($mbox, $uid, $part, $message);
            }
        }

        $this->add_texts('localization/');
        foreach ($parts as $id => $p) {
            if ($p['status'] == 'infected') {
                rcube::write_log('errors', sprintf('patrabahok_guard: infected attachment %s in %s/%s (%s) for %s',
                    $id, $mbox, $uid, $p['virus'], $rcmail->get_user_name()));
            }
        }

        $rcmail->output->command('plugin.pbguard_scan_result', ['uid' => (int) $uid, 'parts' => (object) $parts]);
        $rcmail->output->send();
    }

    /** Download / open of a message part: never serve an infected one. */
    public function message_part_get($args)
    {
        if (!$this->scan_enabled() || empty($args['part'])) {
            return $args;
        }

        $rcmail = rcmail::get_instance();
        $mbox = $rcmail->storage->get_folder();
        $uid = (string) $args['uid'];
        $key = $this->scan_key($mbox, $uid, $args['id']);
        $res = $_SESSION['pbguard_scan'][$key] ?? null;

        // Text and image parts that are part of the message body itself are not
        // attachments the user downloads; scan everything else on first access.
        if (!$res && preg_match('/^[0-9]+$/', $uid)) {
            $res = $this->scan_part($mbox, $uid, $args['part']);
        }

        if ($res && $res['status'] == 'infected') {
            $this->block_page($res);
        }

        return $args;
    }

    /** zipdownload's "download all attachments" must not bypass the block. */
    public function startup($args)
    {
        if ($args['action'] == 'plugin.zipdownload.attachments' && $this->scan_enabled()) {
            $rcmail = rcmail::get_instance();
            $uid = rcube_utils::get_input_string('_uid', rcube_utils::INPUT_GET);
            $mbox = rcube_utils::get_input_string('_mbox', rcube_utils::INPUT_GET, true) ?: $rcmail->storage->get_folder();
            if (preg_match('/^[1-9][0-9]*$/', $uid)) {
                $message = new rcube_message($uid, $mbox);
                foreach ((array) $message->attachments as $part) {
                    $res = $this->scan_part($mbox, $uid, $part, $message);
                    if ($res['status'] == 'infected') {
                        $this->block_page($res);
                    }
                }
            }
        }

        return $args;
    }

    private function block_page($res)
    {
        $this->add_texts('localization/');
        $title = $this->gettext('blockedtitle');
        $text = $this->gettext(['name' => 'blockedtext', 'vars' => ['name' => $res['name'] ?? '', 'virus' => $res['virus']]]);

        while (ob_get_level()) {
            ob_end_clean();
        }

        http_response_code(403);
        header('Content-Type: text/html; charset=' . RCUBE_CHARSET);
        header('Content-Disposition: inline');
        header("Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'");
        header('Cache-Control: no-store');
        echo '<!DOCTYPE html><html><head><meta charset="' . RCUBE_CHARSET . '"><title>' . rcube::Q($title) . '</title>'
            . '<meta name="viewport" content="width=device-width, initial-scale=1">'
            . '<style>body{margin:0;font:15px/1.5 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;background:#f6f7f9;color:#1c2024;'
            . 'display:flex;min-height:100vh;align-items:center;justify-content:center}'
            . '.b{max-width:440px;margin:16px;padding:24px;border-radius:12px;background:#fff;border:1px solid #e5e7eb;border-top:4px solid #e5484d}'
            . 'h1{font-size:18px;margin:0 0 8px;color:#e5484d}p{margin:0;color:#4b5563}'
            . '@media (prefers-color-scheme:dark){body{background:#111316;color:#eceef0}.b{background:#1a1d21;border-color:#2b2f35}p{color:#b0b4ba}}</style>'
            . '</head><body><div class="b" role="alert"><h1>' . rcube::Q($title) . '</h1><p>' . rcube::Q($text) . '</p></div></body></html>';
        exit;
    }
}
