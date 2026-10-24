/**
 * patrabahok Webmail client features. See patrabahok.php for the list.
 *
 * @license GNU GPLv3+ (Roundcube plugin)
 */
(function () {
    'use strict';

    if (!window.rcmail) {
        return;
    }

    var rc = window.rcmail;
    var L = function (name, vars) {
        var s = rc.get_label('patrabahok.' + name);
        if (vars) {
            $.each(vars, function (k, v) { s = s.replace('$' + k, v); });
        }
        return s;
    };
    var isMac = /Mac|iPhone|iPad/.test(navigator.platform);
    var esc = function (s) { return $('<div>').text(s == null ? '' : s).html(); };

    // ------------------------------------------------------------------ avatars
    var PALETTE = ['#2563eb', '#17a558', '#7c3aed', '#f0900f', '#0eaf9b', '#e11d48', '#0b5cad', '#c2410c'];

    function hash(s) {
        var h = 0;
        for (var i = 0; i < s.length; i++) {
            h = (h * 31 + s.charCodeAt(i)) | 0;
        }
        return Math.abs(h);
    }

    function initials(name) {
        name = $.trim((name || '').replace(/<[^>]*>/g, '').replace(/["']/g, ''));
        if (name.indexOf('@') > 0) {
            name = name.split('@')[0].replace(/[._-]+/g, ' ');
        }
        var parts = name.split(/\s+/).filter(Boolean);
        if (!parts.length) {
            return '?';
        }
        var a = parts[0].charAt(0), b = parts.length > 1 ? parts[parts.length - 1].charAt(0) : (parts[0].charAt(1) || '');
        return (a + b).toUpperCase();
    }

    function avatar(name, key, size) {
        var color = PALETTE[hash((key || name || '').toLowerCase()) % PALETTE.length];
        return $('<span class="pb-avatar" aria-hidden="true">')
            .addClass(size ? 'pb-avatar-' + size : '')
            .css('--pb-avatar', color)
            .text(initials(name));
    }

    function decorateRow(row) {
        var tr = row && (row.obj || row);
        if (!tr || !tr.querySelector || tr.querySelector('.pb-avatar')) {
            return;
        }
        var from = tr.querySelector('span.adr span[title], span.adr .rcmContactAddress, .fromto span[title]') || tr.querySelector('.fromto, .from');
        var cell = tr.querySelector('td.subject');
        if (!from || !cell) {
            return;
        }
        var email = from.getAttribute('title') || from.textContent;
        $(cell).prepend(avatar(from.textContent, email));
        tr.classList.add('pb-has-avatar');
    }

    function decorateMessageHeader() {
        var header = document.querySelector('#message-header');
        if (!header || header.querySelector('.pb-avatar')) {
            return;
        }
        var from = header.querySelector('.header.from a.rcmContactAddress, .header-headers .from a, a.rcmContactAddress');
        if (!from) {
            return;
        }
        var title = from.getAttribute('title') || from.getAttribute('href') || '';
        var email = (title.match(/[^\s<>"]+@[^\s<>"]+/) || [from.textContent])[0];
        // Replace Elastic's generic silhouette, but keep a real contact photo.
        var photo = header.querySelector('img.contactphoto');
        if (photo && !/contactpic\.svg/.test(photo.getAttribute('src') || '')) {
            return;
        }
        var av = avatar(from.textContent, email, 'lg');
        if (photo) {
            $(photo).hide().before(av);
        } else {
            $(header).prepend(av);
        }
    }

    // -------------------------------------------------------------- toast / undo
    function toast(html, opts) {
        opts = opts || {};
        var host = $('#pb-toasts');
        if (!host.length) {
            host = $('<div id="pb-toasts" aria-live="polite">').appendTo(document.body);
        }
        var el = $('<div class="pb-toast">').html(html).appendTo(host);
        requestAnimationFrame(function () { el.addClass('show'); });
        if (opts.timeout !== 0) {
            setTimeout(function () { close(el); }, opts.timeout || 4000);
        }
        return el;
    }

    function close(el) {
        el.removeClass('show');
        setTimeout(function () { el.remove(); }, 250);
    }

    var sendConfirmed = false, pendingSend = null;

    function setupUndoSend() {
        var delay = parseInt(rc.env.pb_undo_send, 10) || 0;

        rc.addEventListener('beforesend', function () {
            if (sendConfirmed || delay <= 0) {
                sendConfirmed = false;
                return;
            }
            if (pendingSend) {
                return false; // already counting down
            }
            // Roundcube's own checks (recipients, subject, attachment reminder) run in
            // command('send') after this event, so they still apply when the timer fires.
            var left = delay;
            var el = toast(
                '<span class="pb-toast-icon pb-spin"></span><span class="pb-toast-text"></span>'
                + '<button type="button" class="pb-toast-btn" data-act="undo">' + esc(L('undo')) + '</button>'
                + '<button type="button" class="pb-toast-btn primary" data-act="now">' + esc(L('sendnow')) + '</button>'
                + '<span class="pb-toast-bar"><span></span></span>',
                { timeout: 0 });
            var text = el.find('.pb-toast-text');
            el.find('.pb-toast-bar span').css('animation-duration', delay + 's');
            var tick = function () {
                text.text(L('sendingin', { n: left }));
                left--;
            };
            tick();
            var interval = setInterval(tick, 1000);
            var fire = function () {
                clearInterval(interval);
                clearTimeout(timer);
                pendingSend = null;
                close(el);
                sendConfirmed = true;
                rc.command('send', {});
            };
            var timer = setTimeout(fire, delay * 1000);
            pendingSend = { cancel: function () {
                clearInterval(interval);
                clearTimeout(timer);
                pendingSend = null;
                close(el);
                toast('<span class="pb-toast-icon info"></span><span class="pb-toast-text">' + esc(L('sendcancelled')) + '</span>');
            } };
            el.on('click', '[data-act=undo]', function () { pendingSend && pendingSend.cancel(); });
            el.on('click', '[data-act=now]', fire);
            return false;
        });

        // Leaving the composer during the countdown cancels the send.
        $(window).on('beforeunload', function () {
            if (pendingSend) {
                pendingSend.cancel();
            }
        });
    }

    // ---------------------------------------------------------- command palette
    function paletteItems() {
        var items = [], task = rc.env.task;
        items.push({ icon: 'compose', label: L('gocompose'), hint: 'C', run: function () { rc.command('compose'); } });
        if (task == 'mail' && rc.gui_objects.qsearchbox) {
            items.push({ icon: 'search', label: L('searchmail'), hint: '/', run: function () { $(rc.gui_objects.qsearchbox).focus(); } });
        }
        $.each(rc.env.pb_folders || [], function (i, f) {
            items.push({ icon: 'folder', label: f.name, group: L('gofolder'), run: function () {
                if (task == 'mail' && rc.env.action == '') {
                    rc.command('list', f.id);
                } else {
                    location.href = rc.url('mail/', { _mbox: f.id });
                }
            } });
        });
        items.push({ icon: 'contacts', label: L('gocontacts'), run: function () { rc.command('addressbook'); } });
        items.push({ icon: 'settings', label: L('gosettings'), run: function () { rc.command('settings'); } });
        items.push({ icon: 'identity', label: L('identities'), run: function () { location.href = rc.url('settings/identities'); } });
        if (rc.env.managesieve !== false) {
            items.push({ icon: 'filter', label: L('filters'), run: function () { location.href = rc.url('settings/plugin.managesieve'); } });
        }
        items.push({ icon: 'theme', label: L('toggletheme'), run: function () { $('#taskmenu a.theme').click(); } });
        items.push({ icon: 'keyboard', label: L('showshortcuts'), hint: '?', run: showShortcuts });
        items.push({ icon: 'logout', label: L('signout'), run: function () { rc.command('logout'); } });
        return items;
    }

    function fuzzy(q, s) {
        q = q.toLowerCase();
        s = s.toLowerCase();
        if (s.indexOf(q) >= 0) {
            return 100 - s.indexOf(q);
        }
        var i = 0;
        for (var j = 0; j < s.length && i < q.length; j++) {
            if (s[j] == q[i]) {
                i++;
            }
        }
        return i == q.length ? 10 : -1;
    }

    function openPalette() {
        if ($('#pb-palette').length) {
            return;
        }
        var all = paletteItems(), sel = 0, shown = all;
        var box = $('<div id="pb-palette" role="dialog" aria-modal="true" aria-label="Command palette">'
            + '<div class="pb-palette-panel"><div class="pb-palette-input"><span class="pb-ico pb-ico-search"></span>'
            + '<input type="text" autocomplete="off" spellcheck="false"><kbd>Esc</kbd></div>'
            + '<ul class="pb-palette-list" role="listbox"></ul></div></div>').appendTo(document.body);
        var input = box.find('input').attr('placeholder', L('palette'));
        var list = box.find('ul');

        var render = function () {
            list.empty();
            if (!shown.length) {
                list.append($('<li class="pb-palette-empty">').text(L('nothingfound')));
            }
            $.each(shown, function (i, it) {
                $('<li role="option">').toggleClass('active', i == sel)
                    .append($('<span class="pb-ico">').addClass('pb-ico-' + it.icon))
                    .append($('<span class="pb-palette-label">').text(it.label))
                    .append(it.group ? $('<span class="pb-palette-group">').text(it.group) : null)
                    .append(it.hint ? $('<kbd>').text(it.hint) : null)
                    .on('mousemove', function () { if (sel != i) { sel = i; render(); } })
                    .on('click', function () { run(it); })
                    .appendTo(list);
            });
            var a = list.children('.active')[0];
            a && a.scrollIntoView({ block: 'nearest' });
        };
        var run = function (it) { destroy(); it.run(); };
        var destroy = function () { box.remove(); };

        input.on('input', function () {
            var q = $.trim(this.value);
            sel = 0;
            shown = !q ? all : all.map(function (it) { return { it: it, s: fuzzy(q, it.label + ' ' + (it.group || '')) }; })
                .filter(function (x) { return x.s >= 0; })
                .sort(function (a, b) { return b.s - a.s; })
                .map(function (x) { return x.it; });
            render();
        }).on('keydown', function (e) {
            if (e.key == 'ArrowDown') { sel = Math.min(sel + 1, shown.length - 1); render(); e.preventDefault(); }
            else if (e.key == 'ArrowUp') { sel = Math.max(sel - 1, 0); render(); e.preventDefault(); }
            else if (e.key == 'Enter') { shown[sel] && run(shown[sel]); e.preventDefault(); }
            else if (e.key == 'Escape') { destroy(); e.preventDefault(); }
        });
        box.on('mousedown', function (e) { if (e.target === box[0]) { destroy(); } });
        render();
        setTimeout(function () { input.focus(); }, 0);
    }

    // ---------------------------------------------------------------- shortcuts
    var SHORTCUTS = [
        ['C', 'k_compose'], ['/', 'k_search'], [(isMac ? '⌘' : 'Ctrl') + ' K', 'k_palette'],
        ['J / K', 'k_nextprev'], ['Enter', 'k_open'], ['R', 'k_reply'], ['A', 'k_replyall'],
        ['F', 'k_forward'], ['E', 'k_archive'], ['#', 'k_delete'], ['S', 'k_flag'],
        ['U', 'k_unread'], ['Shift I', 'k_read'], ['G then I', 'k_inbox'],
        [(isMac ? '⌘' : 'Ctrl') + ' Enter', 'k_send'], ['?', 'k_help']
    ];

    function showShortcuts() {
        if ($('#pb-shortcuts').length) {
            return;
        }
        var rows = SHORTCUTS.map(function (s) {
            var keys = s[0].split(' ').map(function (k) { return k == 'then' || k == '/' && s[0].length > 1 ? '<span class="pb-then">' + esc(k) + '</span>' : '<kbd>' + esc(k) + '</kbd>'; }).join(' ');
            return '<li><span>' + esc(L(s[1])) + '</span><span class="pb-keys">' + keys + '</span></li>';
        }).join('');
        var box = $('<div id="pb-shortcuts" role="dialog" aria-modal="true"><div class="pb-palette-panel pb-shortcuts-panel">'
            + '<div class="pb-shortcuts-head"><h3>' + esc(L('shortcuts')) + '</h3><button type="button" class="pb-close" aria-label="Close">×</button></div>'
            + '<ul class="pb-shortcuts-list">' + rows + '</ul></div></div>').appendTo(document.body);
        var destroy = function () { box.remove(); $(document).off('keydown.pbsc'); };
        box.on('mousedown', function (e) { if (e.target === box[0]) { destroy(); } });
        box.find('.pb-close').on('click', destroy);
        $(document).on('keydown.pbsc', function (e) { if (e.key == 'Escape') { destroy(); } });
    }

    function typing(e) {
        var t = e.target;
        return t && (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName));
    }

    var gPressed = 0;

    // Handles a key in the context of the top-level window's rcmail. Message previews
    // render in an iframe with their own copy of this script, which forwards here.
    function handleKey(e, app) {
        if (e.key == 'k' && (e.ctrlKey || e.metaKey)) {
            openPalette();
            return true;
        }
        if (e.key == 'Enter' && (e.ctrlKey || e.metaKey) && app.env.action == 'compose') {
            app.command('send', {});
            return true;
        }
        if (!app.env.pb_shortcuts || e.ctrlKey || e.metaKey || e.altKey || typing(e) || $('.ui-dialog:visible, .popover.show, #pb-palette').length) {
            return false;
        }
        var list = app.message_list, k = e.key;
        if (gPressed && Date.now() - gPressed < 1200) {
            gPressed = 0;
            if (k == 'i') { app.command('list', 'INBOX'); return true; }
        }
        switch (k) {
            case '?': showShortcuts(); return true;
            case 'c': app.command('compose'); return true;
            case 'g': gPressed = Date.now(); return true;
        }
        if (app.env.task != 'mail') {
            return false;
        }
        switch (k) {
            case '/': if (app.gui_objects.qsearchbox) { $(app.gui_objects.qsearchbox).focus(); return true; } break;
            case 'j': if (list) { list.use_arrow_key(40, false); return true; } break;
            case 'k': if (list) { list.use_arrow_key(38, false); return true; } break;
            case 'Enter': if (list && list.get_single_selection()) { app.command('show'); return true; } break;
            case 'r': app.command('reply'); return true;
            case 'a': app.command('reply-all'); return true;
            case 'f': app.command('forward'); return true;
            case 'e': if (app.commands['plugin.archive']) { app.command('plugin.archive'); return true; } break;
            case '#': app.command('delete'); return true;
            case 's': app.command('mark', app.message_list && app.message_list.get_single_selection() && app.message_list.rows[app.message_list.get_single_selection()].flagged ? 'unflagged' : 'flagged'); return true;
            case 'u': app.command('mark', 'unread'); return true;
            case 'I': app.command('mark', 'read'); return true;
        }
        return false;
    }

    function setupShortcuts() {
        $(document).on('keydown', function (e) {
            var top = rc;
            try {
                if (rc.is_framed() && parent.rcmail) {
                    top = parent.rcmail;
                }
            } catch (err) {}
            var handled = top === rc ? handleKey(e, rc) : (parent.pbHandleKey && parent.pbHandleKey(e, top));
            if (handled) {
                e.preventDefault();
                e.stopPropagation();
            }
        });
        window.pbHandleKey = handleKey;
    }

    // ------------------------------------------------------------ filter chips
    var FILTERS = [['ALL', 'all'], ['UNSEEN', 'unread'], ['FLAGGED', 'flagged'], ['UNANSWERED', 'unanswered'], ['ATTACH', 'withattachment']];

    function setupFilterChips() {
        var select = $('#searchfilter');
        var anchor = $('#layout-list > .searchbar, #layout-list #mailsearchform').first();
        if (!select.length || !anchor.length || $('#pb-chips').length) {
            return;
        }
        var attach = select.find('option').filter(function () { return /Content-Type|multipart/i.test(this.value); }).val();
        var chips = $('<div id="pb-chips" role="toolbar" aria-label="Quick filters">');
        $.each(FILTERS, function (i, f) {
            var value = f[0] == 'ATTACH' ? attach : f[0];
            if (!value) {
                return;
            }
            $('<button type="button" class="pb-chip">').text(L(f[1])).attr('data-filter', value)
                .on('click', function () {
                    select.val(value);
                    rc.filter_mailbox(value);
                    sync();
                })
                .appendTo(chips);
        });
        anchor.after(chips);
        var sync = function () {
            var cur = rc.env.search_filter || select.val() || 'ALL';
            chips.children().each(function () { $(this).toggleClass('active', $(this).attr('data-filter') == cur); });
        };
        rc.addEventListener('listupdate', sync);
        select.on('change', sync);
        sync();
    }

    // ----------------------------------------------------------- composer tools
    var EMOJI = ('😀 😃 😄 😁 😊 🙂 😉 😍 🥰 😘 😎 🤩 🤔 🤗 🙏 👍 👎 👏 🙌 💪 👋 🤝 ✌️ 👌 '
        + '❤️ 💙 💚 💛 🧡 💜 🎉 🎊 🎁 🎓 📚 ✏️ 📝 📎 📅 ⏰ ✅ ❌ ⚠️ ⭐ 🔥 💡 🚀 ☕ 🌟 '
        + '😢 😭 😅 😂 🤣 😮 😴 😇 🥳 😬 🙈 💯 🏆 🇧🇩').split(' ');

    function setupComposer() {
        if (rc.env.action != 'compose') {
            return;
        }
        $('body').addClass('pb-compose');

        var bar = $('<div id="pb-compose-tools">');
        var emojiBtn = $('<button type="button" class="pb-tool" aria-haspopup="true">').attr('title', L('emoji'))
            .append('<span class="pb-ico pb-ico-emoji"></span>');
        var focusBtn = $('<button type="button" class="pb-tool">').attr('title', L('focusmode'))
            .append('<span class="pb-ico pb-ico-focus"></span>');
        var count = $('<span class="pb-wordcount">');
        bar.append(emojiBtn, focusBtn, count);

        var target = $('#compose-content .editor-toolbar, #layout-content .formcontent .composebody, #composebody').first();
        $('#composebodycontainer, #compose-content .composebody').first().before(bar);
        if (!bar.parent().length) {
            target.before(bar);
        }

        var picker = $('<div id="pb-emoji" role="dialog" hidden>');
        $.each(EMOJI, function (i, em) {
            $('<button type="button">').text(em).on('click', function () {
                if (rc.editor) {
                    rc.editor.replace(em);
                }
                picker.attr('hidden', true);
            }).appendTo(picker);
        });
        picker.appendTo(bar);
        emojiBtn.on('click', function (e) {
            e.stopPropagation();
            picker.attr('hidden', !picker.attr('hidden') ? true : null);
        });
        $(document).on('click', function (e) { if (!$(e.target).closest('#pb-emoji').length) { picker.attr('hidden', true); } });

        // Borderless fields need placeholders to read as fields.
        $('#compose-subject').attr('placeholder', L('subjectph'));
        $('#compose_to .recipient-input input').attr('placeholder', L('recipients'));
        $('#composebody').attr('placeholder', L('writehere'));

        // Fold the plain/HTML toggle into the tools bar (its own click handler stays).
        var htmlToggle = $('#composebodycontainer .editor-toolbar a.mce-i-html, #composebodycontainer .editor-toolbar a').first();
        if (htmlToggle.length) {
            htmlToggle.addClass('pb-tool pb-html-toggle').attr('title', rc.get_label('htmltoggle'))
                .empty().append('<span class="pb-ico pb-ico-format"></span>').insertAfter(focusBtn);
        }

        focusBtn.on('click', function () {
            $('html').toggleClass('pb-focus-mode');
            focusBtn.toggleClass('active');
        });

        var update = function () {
            var text = '';
            try {
                text = rc.editor ? rc.editor.get_content({ format: 'text', nosig: true, refresh: false }) : $('#composebody').val();
            } catch (err) {
                text = $('#composebody').val() || '';
            }
            var n = $.trim(text || '').split(/\s+/).filter(Boolean).length;
            count.text(n == 1 ? L('word') : L('words', { n: n }));
        };
        setInterval(update, 1500);
        update();
    }

    // ------------------------------------------------------- hover quick actions
    // Elastic shows flag/delete on hover; add Archive and Mark read/unread through its
    // own events (skin-message-list-hover-menu[-init]), acting on the hovered message.
    var hovered = null;

    function setupHoverActions() {
        rc.addEventListener('skin-message-list-hover-menu-init', function (p) {
            var menu = p.menu;
            var mk = function (cls, title, run) {
                return $('<a class="button">').addClass(cls).attr({ title: title, href: '#', role: 'button' })
                    .on('click', function (e) {
                        e.preventDefault();
                        e.stopPropagation();
                        if (hovered) {
                            run(hovered.uid);
                        }
                        menu.css({ top: '-1000px' });
                    });
            };
            var read = mk('pb-read', rc.get_label('markasread'), function (uid) { rc.mark_message('read', uid); });
            var unread = mk('pb-unread', rc.get_label('markasunread'), function (uid) { rc.mark_message('unread', uid); });
            menu.find('span.txt').after(read, unread);
            // The archive plugin registers its command after this event, so check its env.
            if (rc.env.archive_folder) {
                menu.find('a.delete').before(mk('pb-archive', rc.get_label('archive.buttontitle') || 'Archive', function (uid) {
                    rc.message_list.select(uid);
                    rc.command('plugin.archive');
                }));
            }
        });
        rc.addEventListener('skin-message-list-hover-menu', function (p) {
            hovered = p.record;
            var row = rc.message_list && rc.message_list.rows[p.record.uid];
            p.menu.find('.pb-read').toggleClass('d-none', !row || !row.unread);
            p.menu.find('.pb-unread').toggleClass('d-none', !row || !!row.unread);
        });
    }

    // ------------------------------------------------------------- empty state
    function setupEmptyState() {
        if (rc.env.task != 'mail' || rc.env.action) {
            return;
        }
        var wrap = $('#layout-content > .iframe-wrapper');
        if (!wrap.length || $('#pb-empty').length) {
            return;
        }
        var empty = $('<div id="pb-empty"><div class="pb-empty-art"></div><h3></h3><p></p></div>');
        empty.find('h3').text(L('emptytitle'));
        empty.find('p').text(L('emptyhint'));
        wrap.append(empty);
        // Roundcube loads previews by navigating the frame, not by changing its src
        // attribute, so look at what the frame actually shows.
        var frame = $('#messagecontframe');
        var sync = function () {
            var href = '';
            try {
                href = frame[0].contentWindow.location.href;
            } catch (err) {}
            var showing = /_action=(preview|show)|_uid=/.test(href);
            empty.toggle(!showing);
        };
        frame.on('load', sync);
        rc.addEventListener('selectfolder', function () { setTimeout(sync, 50); });
        setInterval(sync, 600);
        sync();
    }

    // ------------------------------------------------------------- palette hint
    function setupTopHint() {
        if (rc.is_framed() || rc.env.action == 'compose') {
            return;
        }
        var btn = $('<button type="button" id="pb-palette-btn">')
            .append('<span class="pb-ico pb-ico-search"></span>')
            .append($('<span class="pb-palette-btn-label">').text(L('palette')))
            .append($('<kbd>').text(isMac ? '⌘K' : 'Ctrl K'))
            .on('click', openPalette);
        $('#layout-menu .popover-header').after(btn);
    }

    rc.addEventListener('init', function () {
        if (rc.env.pb_avatars) {
            rc.addEventListener('insertrow', function (evt) { decorateRow(evt.row); });
            if (rc.message_list) {
                $.each(rc.message_list.rows, function (uid, row) { decorateRow(row); });
            }
            decorateMessageHeader();
        }
        if (!rc.is_framed()) {
            setupHoverActions();
            setupShortcuts();
            setupFilterChips();
            setupEmptyState();
            setupTopHint();
        } else {
            setupShortcuts();
        }
        setupComposer();
        setupUndoSend();
    });
})();
