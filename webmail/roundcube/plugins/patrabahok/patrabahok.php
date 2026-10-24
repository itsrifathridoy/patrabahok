<?php

/**
 * patrabahok: modern features for the patrabahok Webmail (Roundcube engine).
 *
 * - Undo send: a configurable grace period before a message actually goes out
 * - Command palette (Ctrl/Cmd+K) for jumping anywhere and running any action
 * - Keyboard shortcuts in the Gmail style, with a "?" cheat sheet
 * - Sender avatars in the message list and message view
 * - Quick filter chips (Unread, Flagged, With attachments, ...) above the message list
 * - Composer tools: emoji picker, focus mode, live word count, Ctrl/Cmd+Enter to send
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

    public function init()
    {
        $rcmail = rcmail::get_instance();
        $this->load_config();
        $this->add_texts('localization/', true);

        $this->add_hook('render_page', [$this, 'render_page']);
        $this->add_hook('preferences_list', [$this, 'preferences_list']);
        $this->add_hook('preferences_save', [$this, 'preferences_save']);
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

        if ($args['template'] == 'mail' || $args['template'] == 'compose') {
            // Folders for the command palette; Roundcube only exposes a nested tree.
            $folders = [];
            foreach ((array) $rcmail->storage->list_folders_subscribed('', '*', 'mail') as $name) {
                $folders[] = ['id' => $name, 'name' => $rcmail->localize_foldername($name, false, true)];
            }
            $rcmail->output->set_env('pb_folders', $folders);
        }

        return $args;
    }

    public function preferences_list($args)
    {
        $rcmail = rcmail::get_instance();

        if ($args['section'] == 'compose') {
            $select = new html_select(['name' => '_pb_undo_send', 'id' => 'pb_undo_send']);
            foreach ([0, 5, 10, 20, 30] as $s) {
                $select->add($s ? $this->gettext(['name' => 'undoseconds', 'vars' => ['n' => $s]]) : $this->gettext('undooff'), $s);
            }
            $args['blocks']['main']['options']['pb_undo_send'] = [
                'title'   => html::label('pb_undo_send', rcube::Q($this->gettext('undosend'))),
                'content' => $select->show((int) $rcmail->config->get('patrabahok_undo_send', 5)),
            ];
        }

        if ($args['section'] == 'general') {
            foreach (['shortcuts' => 'enableshortcuts', 'avatars' => 'showavatars'] as $key => $label) {
                $check = new html_checkbox(['name' => "_pb_$key", 'id' => "pb_$key", 'value' => 1]);
                $args['blocks']['main']['options']["pb_$key"] = [
                    'title'   => html::label("pb_$key", rcube::Q($this->gettext($label))),
                    'content' => $check->show($rcmail->config->get("patrabahok_$key", true) ? 1 : 0),
                ];
            }
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

        return $args;
    }
}
