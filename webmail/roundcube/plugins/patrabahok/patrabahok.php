<?php

/**
 * patrabahok: modern features for the patrabahok Webmail (Roundcube engine).
 *
 * - Message list previews: a two-line text snippet and attachment chips per message,
 *   fetched in batches (one partial FETCH per distinct text part) and cached
 * - Quick reply from the reading pane, sent through Roundcube's own rcmail_sendmail
 *   (identity, Sent folder, threading headers and the Answered flag all as usual)
 * - Undo send: a configurable grace period before a message actually goes out
 * - Command palette (Ctrl/Cmd+K) and Gmail-style keyboard shortcuts with a "?" sheet
 * - Sender avatars, category tabs (with the patrabahok_guard plugin), a reader pager
 * - Composer: smart replies, emoji, focus mode, word count, floating format toolbar
 *
 * Everything is built on Roundcube's public plugin and client APIs (commands,
 * before<command> events, the editor wrapper), so it degrades to stock behavior if
 * the plugin is disabled.
 *
 * @license GNU GPLv3+ (Roundcube plugin)
 */
class patrabahok extends rcube_plugin
{
    public $task = '?(?!login|logout).*';

    /** Bytes of a text part fetched to build a snippet. */
    private const SNIPPET_BYTES = 2400;
    /** HTML bodies front-load <head>/<style>, so read more of them. */
    private const SNIPPET_HTML_BYTES = 8000;
    private const SNIPPET_CACHE_MAX = 600;

    public function init()
    {
        $this->load_config();
        $this->add_texts('localization/', true);

        $this->add_hook('render_page', [$this, 'render_page']);
        $this->add_hook('preferences_list', [$this, 'preferences_list']);
        $this->add_hook('preferences_save', [$this, 'preferences_save']);

        $this->register_action('plugin.pb-snippets', [$this, 'action_snippets']);
        $this->register_action('plugin.pb-quickreply', [$this, 'action_quickreply']);
    }

    public function render_page($args)
    {
        $rcmail = rcmail::get_instance();
        if ($rcmail->output->type != 'html') {
            return $args;
        }

        // One self-contained stylesheet for every skin (it carries its own tokens).
        $this->include_stylesheet('skins/patrabahok/patrabahok.css');
        $this->include_script('patrabahok.js');

        $rcmail->output->set_env('pb_undo_send', (int) $rcmail->config->get('patrabahok_undo_send', 5));
        $rcmail->output->set_env('pb_shortcuts', (bool) $rcmail->config->get('patrabahok_shortcuts', true));
        $rcmail->output->set_env('pb_avatars', (bool) $rcmail->config->get('patrabahok_avatars', true));
        $rcmail->output->set_env('pb_snippets', (bool) $rcmail->config->get('patrabahok_snippets', true));

        if ($args['template'] == 'mail' || $args['template'] == 'compose') {
            // Folders for the command palette; Roundcube only exposes a nested tree.
            $folders = [];
            foreach ((array) $rcmail->storage->list_folders_subscribed('', '*', 'mail') as $name) {
                $folders[] = ['id' => $name, 'name' => $rcmail->localize_foldername($name, false, true)];
            }
            $rcmail->output->set_env('pb_folders', $folders);
        }

        if ($args['template'] == 'message' && !empty($rcmail->user->ID)) {
            $identity = $rcmail->user->get_identity();
            $rcmail->output->set_env('pb_me', [
                'name' => $identity['name'] ?? '',
                'email' => $identity['email'] ?? '',
            ]);
        }

        return $args;
    }

    // ------------------------------------------------------------------ snippets

    /**
     * Returns preview text and attachment names for up to 100 messages of a folder.
     * Input: _mbox, _uids (comma-separated). Output: plugin.pb_snippets command.
     */
    public function action_snippets()
    {
        $rcmail = rcmail::get_instance();
        $mbox = rcube_utils::get_input_string('_mbox', rcube_utils::INPUT_POST, true);
        $uids = array_slice(array_values(array_unique(array_filter(
            array_map('intval', explode(',', (string) rcube_utils::get_input_string('_uids', rcube_utils::INPUT_POST)))
        ))), 0, 100);

        $result = ['mbox' => $mbox, 'items' => []];
        if ($mbox === '' || empty($uids) || !$rcmail->storage->folder_exists($mbox)) {
            $rcmail->output->command('plugin.pb_snippets', $result);
            $rcmail->output->send();
        }

        $cache = &$_SESSION['patrabahok_snippets'];
        if (!is_array($cache)) {
            $cache = [];
        }
        $key = function ($uid) use ($mbox) { return md5($mbox) . ':' . $uid; };

        $todo = [];
        foreach ($uids as $uid) {
            if (isset($cache[$key($uid)])) {
                $result['items'][$uid] = $cache[$key($uid)];
            } else {
                $todo[] = $uid;
            }
        }

        if (!empty($todo)) {
            foreach ($this->build_snippets($mbox, $todo) as $uid => $item) {
                $result['items'][$uid] = $item;
                $cache[$key($uid)] = $item;
            }
            if (count($cache) > self::SNIPPET_CACHE_MAX) {
                $cache = array_slice($cache, -self::SNIPPET_CACHE_MAX, null, true);
            }
        }

        $rcmail->output->command('plugin.pb_snippets', $result);
        $rcmail->output->send();
    }

    private function build_snippets($mbox, array $uids)
    {
        $rcmail = rcmail::get_instance();
        $storage = $rcmail->get_storage();
        if (!$storage->check_connection()) {
            return [];
        }
        $conn = $storage->conn;

        $items = [];
        $texts = []; // "part id|subtype" => [uid => part meta]
        $html_parts = [];
        $structures = $conn->fetch($mbox, implode(',', $uids), true, ['BODYSTRUCTURE']);
        foreach ((array) $structures as $msg) {
            if (empty($msg->uid)) {
                continue;
            }
            $parts = ['text' => [], 'files' => []];
            if (is_array($msg->bodystructure)) {
                $this->walk_structure($msg->bodystructure, '', $parts);
            }
            $text = $parts['text']['plain'] ?? $parts['text']['html'] ?? null;
            $items[$msg->uid] = ['text' => '', 'files' => array_slice($parts['files'], 0, 4), 'nfiles' => count($parts['files'])];
            if ($text) {
                $texts[$text['id'] . '|' . $text['sub']][$msg->uid] = $text;
            }
            if (isset($parts['text']['plain'], $parts['text']['html'])) {
                $html_parts[$msg->uid] = $parts['text']['html'];
            }
        }

        $this->fetch_snippet_groups($conn, $mbox, $texts, $items);

        // Newsletters often carry a placeholder plain part ("view the HTML version");
        // read their HTML part instead.
        $retry = [];
        foreach ($html_parts as $uid => $part) {
            if (preg_match('/^$|html version|html-version|in (a|your) (web )?browser|html (e-?mail|mail|viewer)|enable html/i', $items[$uid]['text'])) {
                $retry[$part['id'] . '|html'][$uid] = $part;
            }
        }
        if ($retry) {
            $this->fetch_snippet_groups($conn, $mbox, $retry, $items);
        }

        return $items;
    }

    private function fetch_snippet_groups($conn, $mbox, array $texts, array &$items)
    {
        foreach ($texts as $group => $members) {
            [$part_id, $sub] = explode('|', $group);
            $bytes = $sub == 'html' ? self::SNIPPET_HTML_BYTES : self::SNIPPET_BYTES;
            $fetched = $conn->fetch($mbox, implode(',', array_keys($members)), true, ["BODY.PEEK[$part_id]<0.$bytes>"]);
            foreach ((array) $fetched as $msg) {
                if (empty($msg->uid) || !isset($members[$msg->uid]) || empty($msg->bodypart)) {
                    continue;
                }
                $raw = (string) reset($msg->bodypart);
                $items[$msg->uid]['text'] = $this->snippet_text($raw, $members[$msg->uid]);
            }
        }
    }

    /**
     * Walks an IMAP BODYSTRUCTURE (as tokenized by rcube_imap_generic) collecting the
     * first text/plain and text/html parts and the attachment names.
     */
    private function walk_structure(array $s, $id, array &$out)
    {
        if (isset($s[0]) && is_array($s[0])) {
            $n = 1;
            foreach ($s as $sub) {
                if (!is_array($sub)) {
                    break;
                }
                $this->walk_structure($sub, $id === '' ? (string) $n : "$id.$n", $out);
                $n++;
            }

            return;
        }

        $type = strtolower((string) ($s[0] ?? ''));
        $subtype = strtolower((string) ($s[1] ?? ''));
        $params = $this->kv($s[2] ?? null);
        $part_id = $id === '' ? '1' : $id;
        $disp_index = $type == 'text' ? 9 : ($type == 'message' && $subtype == 'rfc822' ? 11 : 8);
        $disp = isset($s[$disp_index]) && is_array($s[$disp_index]) ? $s[$disp_index] : null;
        $disp_type = $disp ? strtolower((string) $disp[0]) : '';
        $disp_params = $disp ? $this->kv($disp[1] ?? null) : [];
        $name = $disp_params['filename'] ?? $params['name'] ?? '';
        if ($name === '' && isset($disp_params['filename*'])) {
            $name = preg_replace("/^[^']*'[^']*'/", '', urldecode($disp_params['filename*']));
        }
        if ($name !== '') {
            $name = rcube_mime::decode_mime_string($name);
        }

        if ($type == 'text' && ($subtype == 'plain' || $subtype == 'html') && $disp_type != 'attachment' && $name === '') {
            if (!isset($out['text'][$subtype])) {
                $out['text'][$subtype] = [
                    'id' => $part_id,
                    'sub' => $subtype,
                    'charset' => $params['charset'] ?? '',
                    'enc' => strtolower((string) ($s[5] ?? '')),
                ];
            }

            return;
        }

        $inline_image = $type == 'image' && $disp_type != 'attachment' && !empty($s[3]) && $s[3] !== 'NIL';
        if (!$inline_image && ($name !== '' || $disp_type == 'attachment' || $type == 'message')) {
            $out['files'][] = [
                'name' => $name !== '' ? $name : ($type == 'message' ? 'message.eml' : "$type/$subtype"),
                'type' => "$type/$subtype",
            ];
        }
    }

    private function kv($list)
    {
        $out = [];
        if (is_array($list)) {
            for ($i = 0; $i + 1 < count($list); $i += 2) {
                if (is_string($list[$i])) {
                    $out[strtolower($list[$i])] = is_string($list[$i + 1]) ? $list[$i + 1] : '';
                }
            }
        }

        return $out;
    }

    private function snippet_text($raw, array $part)
    {
        if ($part['enc'] == 'base64') {
            $raw = preg_replace('/[^A-Za-z0-9+\/=]/', '', $raw);
            $raw = base64_decode(substr($raw, 0, intdiv(strlen($raw), 4) * 4));
        } elseif ($part['enc'] == 'quoted-printable') {
            $raw = quoted_printable_decode(preg_replace('/=[0-9A-Fa-f]?$/', '', $raw));
        }

        $charset = $part['charset'] ?: 'UTF-8';
        $text = rcube_charset::convert((string) $raw, $charset, RCUBE_CHARSET);
        // a multibyte sequence may have been cut at the end
        $text = mb_convert_encoding($text, 'UTF-8', 'UTF-8');

        if ($part['sub'] == 'html') {
            $text = preg_replace('#<(head|style|script|title)\b[^>]*>.*?(</\1>|$)#is', ' ', $text);
            $text = preg_replace('#<br\s*/?>|</(p|div|tr|li|h[1-6])>#i', "\n", $text);
            $text = html_entity_decode(strip_tags($text), ENT_QUOTES | ENT_HTML5, 'UTF-8');
        }

        $lines = [];
        foreach (preg_split('/\r?\n/', $text) as $line) {
            $line = trim($line);
            if ($line === '--' || $line === '-- ') {
                break; // signature
            }
            if ($line === '' || $line[0] === '>' || preg_match('/^(On .{6,} wrote:|-{2,} ?Original Message ?-{2,}|From: .+)$/i', $line)) {
                continue;
            }
            $lines[] = $line;
            if (mb_strlen(implode(' ', $lines)) > 260) {
                break;
            }
        }

        $text = preg_replace('/\s+/u', ' ', implode(' ', $lines));

        return mb_strlen($text) > 220 ? rtrim(mb_substr($text, 0, 217)) . '…' : $text;
    }

    // --------------------------------------------------------------- quick reply

    /**
     * Sends a short plain-text reply to a message from the reading pane.
     * Input: _uid, _mbox, _text, _all (reply to all). Output: plugin.pb_quickreply.
     */
    public function action_quickreply()
    {
        $rcmail = rcmail::get_instance();
        $uid = (int) rcube_utils::get_input_string('_uid', rcube_utils::INPUT_POST);
        $mbox = rcube_utils::get_input_string('_mbox', rcube_utils::INPUT_POST, true);
        $text = trim((string) rcube_utils::get_input_string('_text', rcube_utils::INPUT_POST, true));
        $all = !empty($_POST['_all']);

        $fail = function ($label) use ($rcmail) {
            $rcmail->output->show_message($label, 'error');
            $rcmail->output->command('plugin.pb_quickreply', ['ok' => false]);
            $rcmail->output->send();
        };

        if ($uid <= 0 || $text === '' || !$rcmail->storage->folder_exists($mbox)) {
            $fail('patrabahok.qrinvalid');
        }

        $rcmail->storage->set_folder($mbox);
        $message = new rcube_message($uid, $mbox);
        if (empty($message->headers)) {
            $fail('messageopenerror');
        }

        $identity = rcmail_sendmail::identity_select($message);
        if (empty($identity['identity_id'])) {
            $fail('nofromaddress');
        }

        // recipients: Reply-To/From, plus everyone else on To/Cc for reply-all
        $me = strtolower($identity['email']);
        $reply_to = $message->headers->replyto ?: $message->headers->from;
        $to = [];
        $cc = [];
        foreach (rcube_mime::decode_address_list($reply_to, null, true, $message->headers->charset) as $a) {
            $to[strtolower($a['mailto'])] = $a['string'];
        }
        if ($all) {
            foreach ([$message->headers->to, $message->headers->cc] as $list) {
                foreach (rcube_mime::decode_address_list((string) $list, null, true, $message->headers->charset) as $a) {
                    $addr = strtolower($a['mailto']);
                    if ($addr && $addr != $me && !isset($to[$addr])) {
                        $cc[$addr] = $a['string'];
                    }
                }
            }
        }
        unset($to[$me]);
        if (empty($to)) {
            $fail('patrabahok.qrnorecipient');
        }

        $msgid = (string) $message->headers->messageID;
        $refs = trim((string) $message->headers->references);
        if ($refs === '' && !empty($message->headers->get('in-reply-to'))) {
            $refs = trim($message->headers->get('in-reply-to'));
        }

        $compose = [
            'id' => uniqid(mt_rand()),
            'mode' => rcmail_sendmail::MODE_REPLY,
            'mailbox' => $mbox,
            'reply_uid' => $uid,
            'reply_msgid' => $msgid,
            'references' => trim($refs . ' ' . $msgid),
            'param' => [],
        ];

        // rcmail_sendmail reads the header fields from the request, like the composer
        $_POST['_from'] = $identity['identity_id'];
        $_POST['_to'] = implode(', ', $to);
        $_POST['_cc'] = implode(', ', $cc);
        $_POST['_subject'] = rcmail_sendmail::reply_subject((string) $message->subject);

        $sendmail = new rcmail_sendmail($compose, [
            'sendmail' => true,
            'error_handler' => function (...$args) use ($rcmail) {
                call_user_func_array([$rcmail->output, 'show_message'], $args);
                $rcmail->output->command('plugin.pb_quickreply', ['ok' => false]);
                $rcmail->output->send();
            },
        ]);

        $headers = $sendmail->headers_input();
        $body = $text . "\r\n\r\n" . $this->quote_original($message, $rcmail);
        if (!empty($identity['signature']) && empty($identity['html_signature'])) {
            $body = $text . "\r\n\r\n-- \r\n" . $identity['signature'] . "\r\n\r\n" . $this->quote_original($message, $rcmail);
        }

        $mime = $sendmail->create_message($headers, $body, false, []);
        $plugin = $rcmail->plugins->exec_hook('message_ready', ['message' => $mime]);
        $mime = $plugin['message'];

        if (!$sendmail->deliver_message($mime)) {
            $fail('sendingfailed');
        }
        $sendmail->save_message($mime);
        $rcmail->storage->set_flag($uid, 'ANSWERED', $mbox);

        $rcmail->output->show_message('messagesent', 'confirmation');
        $rcmail->output->command('plugin.pb_quickreply', ['ok' => true, 'uid' => $uid]);
        $rcmail->output->send();
    }

    private function quote_original(rcube_message $message, rcmail $rcmail)
    {
        $body = $message->first_text_part($part, true);
        if ($body === null || $body === '') {
            return '';
        }
        if ($part && $part->ctype_secondary == 'html') {
            $body = $rcmail->html2text($body, ['width' => 0, 'links' => false, 'charset' => RCUBE_CHARSET]);
        }
        $from = $message->get_header('from');
        $date = $rcmail->format_date($message->headers->date, $rcmail->config->get('date_long'));
        $intro = $rcmail->gettext(['name' => 'mailreplyintro', 'vars' => [
            'date' => $date,
            'sender' => rcube_mime::decode_address_list($from, 1, false, $message->headers->charset)[1]['name'] ?? $from,
        ]]);
        $quoted = preg_replace('/^/m', '> ', rtrim(str_replace("\r\n", "\n", $body)));

        return $intro . "\r\n" . str_replace("\n", "\r\n", $quoted);
    }

    // --------------------------------------------------------------- preferences

    public function preferences_list($args)
    {
        $rcmail = rcmail::get_instance();

        if ($args['section'] == 'compose') {
            $select = new html_select(['name' => '_pb_undo_send', 'id' => 'pb_undo_send']);
            foreach ([0, 5, 10, 20, 30] as $s) {
                $select->add($s ? $this->gettext(['name' => 'undoseconds', 'vars' => ['n' => $s]]) : $this->gettext('undooff'), $s);
            }
            $args['blocks']['main']['options']['pb_undo_send'] = [
                'title' => html::label('pb_undo_send', rcube::Q($this->gettext('undosend'))),
                'content' => $select->show((int) $rcmail->config->get('patrabahok_undo_send', 5)),
            ];
        }

        if ($args['section'] == 'general') {
            foreach (['shortcuts' => 'enableshortcuts', 'avatars' => 'showavatars'] as $key => $label) {
                $check = new html_checkbox(['name' => "_pb_$key", 'id' => "pb_$key", 'value' => 1]);
                $args['blocks']['main']['options']["pb_$key"] = [
                    'title' => html::label("pb_$key", rcube::Q($this->gettext($label))),
                    'content' => $check->show($rcmail->config->get("patrabahok_$key", true) ? 1 : 0),
                ];
            }
        }

        if ($args['section'] == 'mailbox') {
            $check = new html_checkbox(['name' => '_pb_snippets', 'id' => 'pb_snippets', 'value' => 1]);
            $args['blocks']['main']['options']['pb_snippets'] = [
                'title' => html::label('pb_snippets', rcube::Q($this->gettext('showsnippets'))),
                'content' => $check->show($rcmail->config->get('patrabahok_snippets', true) ? 1 : 0),
            ];
        }

        return $args;
    }

    public function preferences_save($args)
    {
        if ($args['section'] == 'compose') {
            $n = (int) rcube_utils::get_input_string('_pb_undo_send', rcube_utils::INPUT_POST);
            $args['prefs']['patrabahok_undo_send'] = in_array($n, [0, 5, 10, 20, 30]) ? $n : 5;
        }
        if ($args['section'] == 'general') {
            $args['prefs']['patrabahok_shortcuts'] = !empty($_POST['_pb_shortcuts']);
            $args['prefs']['patrabahok_avatars'] = !empty($_POST['_pb_avatars']);
        }
        if ($args['section'] == 'mailbox') {
            $args['prefs']['patrabahok_snippets'] = !empty($_POST['_pb_snippets']);
        }

        return $args;
    }
}
