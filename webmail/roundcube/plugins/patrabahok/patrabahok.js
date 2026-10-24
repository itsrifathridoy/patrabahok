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
    var store = {
        get: function (k) { try { return window.localStorage.getItem(k); } catch (e) { return null; } },
        set: function (k, v) { try { window.localStorage.setItem(k, v); } catch (e) {} }
    };
    var topApp = function () {
        try {
            if (rc.is_framed() && parent.rcmail) {
                return parent.rcmail;
            }
        } catch (e) {}
        return rc;
    };

    // ------------------------------------------------------------------ avatars
    var PALETTE = ['#5b5bf0', '#0ea5e9', '#22a06b', '#d98a0b', '#8b5cf6', '#e5484d', '#0f766e', '#c2410c', '#db2777'];

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

    // ------------------------------------------------------------- toast / undo
    function toast(html, opts) {
        opts = opts || {};
        var host = $('#pb-toasts');
        if (!host.length) {
            host = $('<div id="pb-toasts" aria-live="polite">').appendTo(document.body);
        }
        var el = $('<div class="pb-toast">').html(html).appendTo(host);
        requestAnimationFrame(function () { el.addClass('show'); });
        if (opts.timeout !== 0) {
            setTimeout(function () { closeToast(el); }, opts.timeout || 4000);
        }
        return el;
    }
    window.pbToast = toast;

    function closeToast(el) {
        el.removeClass('show');
        setTimeout(function () { el.remove(); }, 250);
    }

    /**
     * Runs fire() after the user's undo-send delay, showing a countdown toast with
     * Undo / Send now. Returns false when there is no delay (caller sends directly).
     */
    function countdown(fire, onUndo) {
        var delay = parseInt(rc.env.pb_undo_send, 10) || 0;
        if (delay <= 0) {
            return false;
        }
        var t = (topApp() !== rc && parent.pbToast) ? parent.pbToast : toast;
        var left = delay;
        var el = t(
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
        var done = function () {
            clearInterval(interval);
            clearTimeout(timer);
            closeToast(el);
        };
        var go = function () { done(); fire(); };
        var timer = setTimeout(go, delay * 1000);
        var cancel = function () {
            done();
            t('<span class="pb-toast-icon info"></span><span class="pb-toast-text">' + esc(L('sendcancelled')) + '</span>');
            onUndo && onUndo();
        };
        el.on('click', '[data-act=undo]', cancel);
        el.on('click', '[data-act=now]', go);
        return { cancel: cancel };
    }

    var sendConfirmed = false, pendingSend = null;

    function setupUndoSend() {
        rc.addEventListener('beforesend', function () {
            if (sendConfirmed) {
                sendConfirmed = false;
                return;
            }
            if (pendingSend) {
                return false; // already counting down
            }
            // Roundcube's own checks (recipients, subject, attachment reminder) run in
            // command('send') after this event, so they still apply when the timer fires.
            var c = countdown(function () {
                pendingSend = null;
                sendConfirmed = true;
                rc.command('send', {});
            }, function () { pendingSend = null; });
            if (!c) {
                return;
            }
            pendingSend = c;
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
        $.each(rc.env.pb_categories || [], function (i, c) {
            items.push({ icon: 'tag', label: c.label, group: L('categories'), run: function () { openCategory(c.id); } });
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
        if (app.env.task != 'mail' || app.env.action == 'compose') {
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
            var top = topApp();
            var handled = top === rc ? handleKey(e, rc) : (parent.pbHandleKey && parent.pbHandleKey(e, top));
            if (handled) {
                e.preventDefault();
                e.stopPropagation();
            }
        });
        window.pbHandleKey = handleKey;
    }

    // ----------------------------------------------------- filters and categories
    var CAT_COLORS = { primary: '#5b5bf0', social: '#0ea5e9', promotions: '#22a06b', updates: '#d98a0b', forums: '#8b5cf6' };
    var QUICK = [['', 'all'], ['UNSEEN', 'unread'], ['FLAGGED', 'flagged'], ['UNANSWERED', 'unanswered'], ['ATTACH', 'withattachment']];
    var catCounts = {}, pendingCat = null;

    function categories() {
        return rc.env.pb_categories_enabled === false ? [] : (rc.env.pb_categories || []);
    }

    function attachFilter() {
        return $('#searchfilter option').filter(function () { return /Content-Type|multipart/i.test(this.value); }).val() || '';
    }

    // Splits the active IMAP filter into its category and quick-filter parts.
    function filterParts() {
        var f = $.trim(rc.env.search_filter || 'ALL'), cat = null;
        $.each(categories(), function (i, c) {
            if (c.filter && (f === c.filter || f.indexOf(c.filter + ' ') === 0)) {
                cat = c;
            }
        });
        var quick = cat ? $.trim(f.slice(cat.filter.length)) : (f == 'ALL' ? '' : f);
        return { cat: cat, quick: quick };
    }

    function applyFilter(cat, quick) {
        var parts = [];
        if (cat && cat.filter) {
            parts.push(cat.filter);
        }
        if (quick) {
            parts.push(quick);
        }
        var f = parts.join(' ') || 'ALL';
        // keep Roundcube's own filter select in step, so a text search keeps the filter
        var select = $('#searchfilter');
        if (select.length) {
            if (!select.find('option').filter(function () { return this.value === f; }).length) {
                select.find('option.pb-dyn').remove();
                select.append($('<option class="pb-dyn">').val(f).text(cat ? cat.label : f));
            }
            select.val(f);
        }
        rc.filter_mailbox(f);
    }

    function openCategory(id) {
        var cat = null;
        $.each(categories(), function (i, c) { if (c.id == id) { cat = c; } });
        if (rc.env.task != 'mail' || rc.env.action) {
            location.href = rc.url('mail/', { _mbox: 'INBOX' }) + (id ? '#pbcat=' + encodeURIComponent(id) : '');
            return;
        }
        if (rc.env.mailbox != 'INBOX') {
            pendingCat = id || '';
            rc.command('list', 'INBOX');
            return;
        }
        applyFilter(cat, filterParts().quick);
    }

    function renderCategoryUI() {
        var cats = categories(), inbox = rc.env.mailbox == 'INBOX', parts = filterParts();
        var tabs = $('#pb-tabs'), labels = $('#pb-labels');

        if (!cats.length) {
            tabs.attr('hidden', true);
            labels.attr('hidden', true);
            return;
        }
        var count = function (c) {
            var n = catCounts[c.id];
            return n ? $('<span class="pb-count">').text(n > 99 ? '99+' : n) : null;
        };

        tabs.empty().attr('hidden', inbox ? null : true);
        $('<button type="button" role="tab">').text(L('alltab')).toggleClass('active', !parts.cat)
            .on('click', function () { applyFilter(null, parts.quick); }).appendTo(tabs);
        $.each(cats, function (i, c) {
            $('<button type="button" role="tab">').css('--pb-cat', CAT_COLORS[c.id] || '#5b5bf0')
                .toggleClass('active', !!parts.cat && parts.cat.id == c.id)
                .append('<span class="pb-dot"></span>', $('<span>').text(c.label), count(c))
                .on('click', function () { applyFilter(c, filterParts().quick); })
                .appendTo(tabs);
        });

        labels.empty().attr('hidden', null)
            .append($('<div class="pb-labels-title">').text(L('categories')));
        $.each(cats, function (i, c) {
            $('<a href="#" role="button">').css('--pb-cat', CAT_COLORS[c.id] || '#5b5bf0')
                .toggleClass('active', inbox && !!parts.cat && parts.cat.id == c.id)
                .append('<span class="pb-dot"></span>', $('<span class="pb-name">').text(c.label),
                    $('<span class="pb-count">').text(catCounts[c.id] || ''))
                .on('click', function (e) { e.preventDefault(); openCategory(c.id); })
                .appendTo(labels);
        });
    }

    function setupFilters() {
        var header = $('#layout-list > .header');
        if (!header.length || $('.pb-filter-btn').length) {
            return;
        }
        var btn = $('<button type="button" class="pb-filter-btn">').attr({ title: L('quickfilter'), 'aria-haspopup': 'true' })
            .append($('<span class="inner">').text(L('quickfilter')));
        header.find('a.refresh').before(btn);

        var menu = null;
        var close = function () { menu && menu.remove(); menu = null; $(document).off('mousedown.pbfm keydown.pbfm'); };
        btn.on('click', function (e) {
            e.stopPropagation();
            if (menu) {
                return close();
            }
            var parts = filterParts();
            menu = $('<div id="pb-filter-menu" role="menu">').appendTo(document.body);
            $.each(QUICK, function (i, q) {
                var value = q[0] == 'ATTACH' ? attachFilter() : q[0];
                if (q[0] == 'ATTACH' && !value) {
                    return;
                }
                $('<button type="button" role="menuitemradio">').text(L(q[1]))
                    .attr('data-filter', value || 'ALL')
                    .toggleClass('active', (parts.quick || '') === value)
                    .on('click', function () { close(); applyFilter(filterParts().cat, value); })
                    .appendTo(menu);
            });
            var r = btn[0].getBoundingClientRect();
            menu.css({ top: r.bottom + 6, left: Math.max(8, Math.min(r.right - 210, window.innerWidth - 218)) });
            $(document).on('mousedown.pbfm', function (ev) { if (!$(ev.target).closest('#pb-filter-menu, .pb-filter-btn').length) { close(); } })
                .on('keydown.pbfm', function (ev) { if (ev.key == 'Escape') { close(); } });
        });

        var sync = function () {
            var parts = filterParts();
            btn.toggleClass('active', !!parts.quick);
            renderCategoryUI();
            if (pendingCat !== null && rc.env.mailbox == 'INBOX') {
                var id = pendingCat;
                pendingCat = null;
                if (id) {
                    openCategory(id);
                }
            }
        };
        rc.addEventListener('listupdate', sync);
        rc.addEventListener('pbguard-categories', function (data) {
            catCounts = (data && data.counts) || {};
            renderCategoryUI();
        });
        var m = /#pbcat=([a-z]+)/.exec(location.hash);
        if (m && rc.env.mailbox == 'INBOX') {
            pendingCat = m[1];
            history.replaceState && history.replaceState(null, '', location.pathname + location.search);
        }
        sync();
    }

    // ------------------------------------------------------- list previews (snippets)
    var snipCache = {}, snipBusy = false, snipQueue = [];

    function fileKind(f) {
        var t = (f.type || '') + ' ' + (f.name || '');
        if (/image\//i.test(t) || /\.(png|jpe?g|gif|webp|svg|heic)$/i.test(f.name)) { return 'img'; }
        if (/pdf/i.test(t)) { return 'pdf'; }
        if (/zip|rar|7z|tar|gzip/i.test(t)) { return 'zip'; }
        return '';
    }

    function renderSnippet(uid, item) {
        var row = rc.message_list && rc.message_list.rows[uid];
        var cell = row && row.obj && row.obj.querySelector('td.subject');
        if (!cell || cell.querySelector('.pb-snippet')) {
            return;
        }
        if (item.text) {
            $('<span class="pb-snippet">').text(item.text).appendTo(cell);
        }
        if (item.files && item.files.length) {
            var chips = $('<span class="pb-chips-row">');
            $.each(item.files.slice(0, 2), function (i, f) {
                $('<span class="pb-chip-file">').addClass(fileKind(f)).attr('title', f.name).text(f.name).appendTo(chips);
            });
            if (item.nfiles > 2) {
                $('<span class="pb-chip-file more">').text('+' + (item.nfiles - 2)).appendTo(chips);
            }
            chips.appendTo(cell);
        }
        row.obj.classList.add('pb-has-snippet');
    }

    function requestSnippets() {
        if (!rc.env.pb_snippets || !rc.message_list || snipBusy) {
            return;
        }
        var mbox = rc.env.mailbox, need = [];
        $.each(rc.message_list.rows, function (uid, row) {
            if (!row.obj || row.obj.classList.contains('pb-has-snippet') || !/^\d+$/.test(uid)) {
                return;
            }
            var hit = snipCache[mbox + '/' + uid];
            if (hit) {
                renderSnippet(uid, hit);
            } else if (snipQueue.indexOf(uid) < 0) {
                need.push(uid);
            }
        });
        if (!need.length) {
            return;
        }
        snipBusy = true;
        snipQueue = need.slice(0, 60);
        rc.http_post('plugin.pb-snippets', { _mbox: mbox, _uids: snipQueue.join(',') }, false);
    }

    function setupSnippets() {
        if (!rc.env.pb_snippets || !rc.message_list) {
            return;
        }
        rc.addEventListener('plugin.pb_snippets', function (data) {
            snipBusy = false;
            snipQueue = [];
            if (!data || !data.items) {
                return;
            }
            $.each(data.items, function (uid, item) {
                snipCache[data.mbox + '/' + uid] = item;
                if (data.mbox == rc.env.mailbox) {
                    renderSnippet(uid, item);
                }
            });
            // more rows may have arrived meanwhile (paging, new mail)
            setTimeout(requestSnippets, 50);
        });
        var t = null;
        var later = function () { clearTimeout(t); t = setTimeout(requestSnippets, 120); };
        rc.addEventListener('listupdate', later);
        rc.addEventListener('insertrow', later);
        later();
    }

    // -------------------------------------------------------------- reader pager
    function setupPager() {
        var header = $('#layout-content > .header');
        var list = rc.message_list;
        if (!header.length || !list || $('.pb-pager').length) {
            return;
        }
        var pager = $('<div class="pb-pager" hidden>')
            .append($('<button type="button" class="prev">').attr('title', rc.get_label('previousmessage')))
            .append('<span class="pb-pager-text"></span>')
            .append($('<button type="button" class="next">').attr('title', rc.get_label('nextmessage')));
        header.append(pager);
        pager.find('.prev').on('click', function () { list.select_prev(); });
        pager.find('.next').on('click', function () { list.select_next(); });

        var update = function () {
            var uid = list.get_single_selection();
            var rows = $(list.tbody).children('tr:visible');
            if (!uid || !list.rows[uid]) {
                pager.attr('hidden', true);
                return;
            }
            var idx = rows.index(list.rows[uid].obj);
            var size = parseInt(rc.env.pagesize, 10) || rows.length;
            var page = parseInt(rc.env.current_page, 10) || 1;
            var total = parseInt(rc.env.messagecount, 10) || rows.length;
            pager.attr('hidden', null);
            pager.find('.pb-pager-text').text(L('pagerof', { n: (page - 1) * size + idx + 1, total: total }));
            pager.find('.prev').prop('disabled', idx <= 0);
            pager.find('.next').prop('disabled', idx >= rows.length - 1);
        };
        list.addEventListener('select', function () { setTimeout(update, 0); });
        rc.addEventListener('listupdate', update);
        update();
    }

    // ------------------------------------------------------- reader (message view)
    function formatWhen(raw) {
        var d = new Date(String(raw).replace(/^(\d{4}-\d{2}-\d{2}) /, '$1T'));
        if (isNaN(d.getTime())) {
            return raw;
        }
        var now = new Date(), opts = { hour: 'numeric', minute: '2-digit' };
        var sameDay = d.toDateString() == now.toDateString();
        var label = sameDay ? L('today') + ', ' + d.toLocaleTimeString([], opts)
            : d.toLocaleString([], $.extend({ weekday: 'short', month: 'short', day: 'numeric' }, d.getFullYear() != now.getFullYear() ? { year: 'numeric' } : {}, opts));
        var diff = (d - now) / 1000, abs = Math.abs(diff), rel = '';
        if (window.Intl && Intl.RelativeTimeFormat && abs < 86400 * 30) {
            var f = new Intl.RelativeTimeFormat([], { numeric: 'auto' });
            rel = abs < 3600 ? f.format(Math.round(diff / 60), 'minute') : abs < 86400 ? f.format(Math.round(diff / 3600), 'hour') : f.format(Math.round(diff / 86400), 'day');
        }
        return rel ? label + ' (' + rel + ')' : label;
    }

    function setupReader() {
        var head = $('#message-header');
        var row = head.find('.pb-sender-row');
        if (!head.length || !row.length) {
            return;
        }
        var from = head.find('.header-headers td.from a.rcmContactAddress, .header-headers .from a.rcmContactAddress').first();
        if (!from.length) {
            from = head.find('.header-summary a.rcmContactAddress').first();
        }
        var email = from.attr('title') || (from.attr('href') || '').replace(/^mailto:/, '') || from.text();
        var name = $.trim(from.text()) || email;
        var photo = head.find('img.contactphoto');
        var av = photo.length && !/contactpic\.svg/.test(photo.attr('src') || '')
            ? $('<span class="pb-avatar pb-avatar-lg pb-avatar-photo">').append(photo.clone().show())
            : avatar(name, email, 'lg');

        var nameEl = $('<div class="pb-sender-name">');
        if (from.length) {
            nameEl.append(from.clone(true).text(name));
        } else {
            nameEl.text(name);
        }
        var guard = rc.env.pb_guard || {};
        if (guard.verified) {
            nameEl.append($('<span class="pb-verified">').attr('title', L('verifiedsender')));
        }
        var to = head.find('.header-headers td.to .adr').map(function () { return $.trim($(this).text()); }).get();
        var addr = $('<div class="pb-sender-addr">').text(email && email != name ? email : '');
        if (to.length) {
            addr.append($('<span class="pb-sender-to">').text((email && email != name ? ' · ' : '') + L('tolist', { list: to.slice(0, 3).join(', ') + (to.length > 3 ? ' +' + (to.length - 3) : '') })));
        }
        var when = $.trim(head.find('.header-summary .text-nowrap').last().text()) || $.trim(head.find('.header-headers td.date').text());
        row.empty().append(av, $('<div class="pb-sender">').append(nameEl, addr), $('<div class="pb-sender-date">').text(formatWhen(when)))
            .attr('hidden', null);

        var n = $('#attachment-list > li').length;
        if (n) {
            $('#message-content .leftcol .pb-section-title').append($('<span class="pb-count-muted">').text(' · ' + n));
        }
    }

    function setupQuickReply() {
        var box = $('#pb-quickreply');
        if (!box.length || !rc.env.uid || rc.env.mailbox == rc.env.drafts_mailbox) {
            return;
        }
        var app = topApp();
        var actions = $('<div class="pb-qr-actions">')
            .append($('<button type="button" class="one">').text(L('qr_reply')))
            .append($('<button type="button" class="all">').text(L('qr_replyall')))
            .append($('<button type="button" class="fwd">').text(L('qr_forward')));
        box.empty().append(actions).attr('hidden', null);

        actions.find('.fwd').on('click', function () { app.command('forward'); });
        actions.find('.one, .all').on('click', function () { open($(this).is('.all')); });

        function open(all) {
            actions.attr('hidden', true);
            var me = rc.env.pb_me || {};
            var from = $('#message-header .pb-sender-name a').attr('title') || $.trim($('#message-header .pb-sender-name').text());
            var card = $('<div class="pb-qr-box">')
                .append(avatar(me.name || me.email, me.email))
                .append('<div class="pb-qr-card"><div class="pb-qr-head"><span class="pb-qr-mode"></span><span class="pb-qr-to"></span>'
                    + '<span class="pb-qr-spacer"></span><button type="button" class="pb-qr-icon expand"></button><button type="button" class="pb-qr-icon close"></button></div>'
                    + '<textarea rows="4"></textarea><div class="pb-qr-foot"><span class="pb-qr-hint"></span><button type="button" class="pb-btn-primary"></button></div></div>');
            card.find('.pb-qr-mode').text(L(all ? 'qr_replyall' : 'qr_reply'));
            card.find('.pb-qr-to').text(from).attr('title', from);
            card.find('.expand').attr('title', L('qrexpand'));
            card.find('.close').attr('title', L('qr_close'));
            card.find('.pb-qr-hint').text(L('qrhint', { key: isMac ? '⌘' : 'Ctrl' }));
            card.find('.pb-btn-primary').text(L('qr_send'));
            var ta = card.find('textarea').attr('placeholder', L('qrplaceholder'));
            box.append(card);
            ta.focus();
            card[0].scrollIntoView({ block: 'nearest', behavior: 'smooth' });

            var shut = function () { card.remove(); actions.attr('hidden', null); };
            card.find('.close').on('click', shut);
            card.find('.expand').on('click', function () {
                try { window.sessionStorage.setItem('pb_qr_text', ta.val()); } catch (e) {}
                app.command(all ? 'reply-all' : 'reply');
            });
            var sendBtn = card.find('.pb-btn-primary');
            var post = function () {
                var lock = rc.set_busy(true, 'sendingmessage');
                sendBtn.prop('disabled', true);
                rc.http_post('plugin.pb-quickreply', { _uid: rc.env.uid, _mbox: rc.env.mailbox, _text: ta.val(), _all: all ? 1 : 0 }, lock);
            };
            var send = function () {
                if (!$.trim(ta.val())) {
                    ta.focus();
                    return;
                }
                sendBtn.prop('disabled', true);
                if (!countdown(post, function () { sendBtn.prop('disabled', false); })) {
                    post();
                }
            };
            sendBtn.on('click', send);
            ta.on('keydown', function (e) {
                if (e.key == 'Enter' && (e.ctrlKey || e.metaKey)) {
                    e.preventDefault();
                    e.stopPropagation();
                    send();
                }
            });
            rc.addEventListener('plugin.pb_quickreply', function (res) {
                sendBtn.prop('disabled', false);
                if (res && res.ok) {
                    card.remove();
                    box.append($('<div class="pb-qr-sent">').text(L('qrsent')));
                    try {
                        if (app.set_message) {
                            app.set_message(rc.env.uid, 'replied', true);
                        }
                    } catch (e) {}
                }
            });
        }
    }

    // ----------------------------------------------------------------- composer
    var EMOJI = ('😀 😃 😄 😁 😊 🙂 😉 😍 🥰 😘 😎 🤩 🤔 🤗 🙏 👍 👎 👏 🙌 💪 👋 🤝 ✌️ 👌 '
        + '❤️ 💙 💚 💛 🧡 💜 🎉 🎊 🎁 🎓 📚 ✏️ 📝 📎 📅 ⏰ ✅ ❌ ⚠️ ⭐ 🔥 💡 🚀 ☕ 🌟 '
        + '😢 😭 😅 😂 🤣 😮 😴 😇 🥳 😬 🙈 💯 🏆 🇧🇩').split(' ');

    function isHtml() {
        return !!(rc.editor && rc.editor.is_html && rc.editor.is_html());
    }

    function insertText(text) {
        if (rc.editor) {
            rc.editor.replace(isHtml() ? esc(text) : text);
        }
    }

    function setupEditorConfig() {
        // Runs before TinyMCE starts: floating selection toolbar, quieter chrome,
        // and editor content that follows the light/dark theme.
        rc.addEventListener('editor-init', function (e) {
            if (rc.env.action != 'compose' || !e || !e.config) {
                return;
            }
            var c = e.config, dark = $('html').hasClass('dark-mode');
            if (c.plugins.indexOf('quickbars') < 0) {
                c.plugins += ' quickbars';
            }
            c.quickbars_insert_toolbar = false;
            c.quickbars_selection_toolbar = 'bold italic underline strikethrough | quicklink h2 h3 blockquote | forecolor removeformat';
            c.skin = dark ? 'oxide-dark' : 'oxide';
            c.content_style = (c.content_style || '')
                + 'body{font-family:"Plus Jakarta Sans",ui-sans-serif,system-ui,-apple-system,"Segoe UI",Roboto,sans-serif;'
                + 'font-size:15px;line-height:1.65;margin:6px 2px;color:' + (dark ? '#ececf1' : '#17181c') + ';background:' + (dark ? '#1c1c1f' : '#ffffff') + '}'
                + 'a{color:' + (dark ? '#9a9aff' : '#5b5bf0') + '}blockquote{border-left:3px solid ' + (dark ? '#3d3d63' : '#d9d9fc') + ';margin-left:0;padding-left:12px;color:' + (dark ? '#9b9ba6' : '#6c707c') + '}'
                + 'p{margin:0 0 .7em}';
        });
    }

    function setupComposer() {
        if (rc.env.action != 'compose') {
            return;
        }
        $('body').addClass('pb-compose');

        // From identity as a pill with an avatar
        var fromSel = $('#_from');
        var fromGroup = $('#compose_from .input-group');
        var paintFrom = function () {
            var t = fromSel.find('option:selected').text();
            fromGroup.find('.pb-avatar').remove();
            fromGroup.prepend(avatar(t, (t.match(/[^\s<>"]+@[^\s<>"]+/) || [t])[0], 'sm'));
        };
        if (fromSel.is('select')) {
            fromSel.on('change', paintFrom);
            paintFrom();
        }

        // Borderless fields need placeholders to read as fields.
        $('#compose-subject').attr('placeholder', L('subjectph'));
        $('#compose_to .recipient-input input').attr('placeholder', L('recipients'));
        $('#composebody').attr('placeholder', L('writehere'));

        var end = $('.pb-sendbar .pb-sendbar-end');
        var status = $('.pb-sendbar .pb-status');
        var mk = function (cls, title) {
            return $('<button type="button" class="pb-bar-btn">').addClass(cls).attr('title', title).attr('aria-label', title);
        };
        var fmtBtn = mk('pb-format', L('formatting'));
        var emojiBtn = mk('pb-emoji-btn', L('emoji')).attr('aria-haspopup', 'true');
        var focusBtn = mk('pb-focus-btn', L('focusmode'));
        end.prepend(fmtBtn, emojiBtn, focusBtn);

        // emoji picker, floating above the button
        var picker = $('<div id="pb-emoji" role="dialog" hidden>').appendTo(document.body);
        $.each(EMOJI, function (i, em) {
            $('<button type="button">').text(em).on('click', function () {
                if (rc.editor) {
                    rc.editor.replace(em);
                }
                picker.attr('hidden', true);
            }).appendTo(picker);
        });
        emojiBtn.on('click', function (e) {
            e.stopPropagation();
            if (picker.attr('hidden')) {
                var r = this.getBoundingClientRect();
                picker.attr('hidden', null).css({
                    left: Math.max(8, Math.min(r.left - 120, window.innerWidth - picker.outerWidth() - 8)),
                    top: Math.max(8, r.top - picker.outerHeight() - 10)
                });
            } else {
                picker.attr('hidden', true);
            }
        });
        $(document).on('click', function (e) { if (!$(e.target).closest('#pb-emoji, .pb-emoji-btn').length) { picker.attr('hidden', true); } });

        // formatting toolbar: hidden by default (select text for the floating bar)
        var container = $('#composebodycontainer');
        var tbShown = store.get('pb_tb') == '1';
        var paintTb = function () {
            container.toggleClass('pb-tb-hidden', !tbShown);
            fmtBtn.toggleClass('pb-on', isHtml() && tbShown);
        };
        fmtBtn.on('click', function (e) {
            if (!isHtml()) {
                rc.command('toggle-editor', { id: 'composebody', html: true }, '', e.originalEvent);
                tbShown = true;
            } else {
                tbShown = !tbShown;
            }
            store.set('pb_tb', tbShown ? '1' : '0');
            setTimeout(paintTb, 50);
        });
        rc.addEventListener('editor-load', paintTb);
        paintTb();

        focusBtn.on('click', function () {
            $('html').toggleClass('pb-focus-mode');
            focusBtn.toggleClass('pb-on');
        });

        // options panel (wide screens slide it in; small screens use Elastic's pane)
        $('.pb-options-toggle').on('click', function (e) {
            e.preventDefault();
            if (window.UI && UI.get_screen_mode && /small|phone/.test(UI.get_screen_mode())) {
                UI.show_sidebar && UI.show_sidebar();
            } else {
                $('body').toggleClass('pb-options-open');
            }
            $(this).toggleClass('pb-on', $('body').hasClass('pb-options-open'));
        });
        $('.pb-options-close').on('click', function (e) {
            e.preventDefault();
            $('body').removeClass('pb-options-open');
            $('.pb-options-toggle').removeClass('pb-on');
        });

        // send menu: skip the undo delay
        $(document).on('click', '.pb-send-instant', function (e) {
            e.preventDefault();
            sendConfirmed = true;
            rc.command('send', {});
        });

        // discard: confirm, drop the autosaved draft, leave
        $('.pb-discard').on('click', function (e) {
            e.preventDefault();
            rc.simple_dialog(L('discardconfirm'), L('discard'), function () {
                rc.compose_skip_unsavedcheck = true;
                if (rc.env.draft_id && rc.env.drafts_mailbox) {
                    rc.http_post('mail/delete', { _uid: rc.env.draft_id, _mbox: rc.env.drafts_mailbox }, false);
                }
                if (rc.env.extwin) {
                    setTimeout(function () { window.close(); }, 300);
                } else {
                    setTimeout(function () { rc.redirect(rc.url('mail/', { _mbox: rc.env.mailbox || 'INBOX' })); }, 300);
                }
                return true;
            }, { button: 'delete', button_class: 'delete' });
        });

        // word count in the bar
        var update = function () {
            var text = '';
            try {
                text = rc.editor ? rc.editor.get_content({ format: 'text', nosig: true, refresh: false }) : $('#composebody').val();
            } catch (err) {
                text = $('#composebody').val() || '';
            }
            var n = $.trim(text || '').split(/\s+/).filter(Boolean).length;
            status.text(n == 1 ? L('word') : L('words', { n: n }));
        };
        setInterval(update, 1500);
        update();

        $('#compose-attachments').attr('data-pb-drop', L('dropfiles'));
        setupSmartReplies();
        restoreQuickReply();
        setupAutocompleteAvatars();
    }

    function setupSmartReplies() {
        var box = $('#pb-smart-replies');
        if (!box.length) {
            return;
        }
        var chips = [];
        if (rc.env.compose_mode == 'reply') {
            chips = ['sr1', 'sr2', 'sr3', 'sr4'].map(function (k) { return { text: L(k) }; });
        }
        $('#responseslist li a').not('.placeholder, .disabled').slice(0, 3).each(function () {
            var a = $(this);
            chips.push({ text: $.trim(a.text()), response: a });
        });
        if (!chips.length) {
            return;
        }
        $.each(chips, function (i, c) {
            $('<button type="button">').text(c.text).toggleClass('pb-sr-response', !!c.response)
                .on('click', function () {
                    if (c.response) {
                        c.response.trigger('click');
                    } else {
                        insertText(c.text);
                    }
                })
                .appendTo(box);
        });
        box.attr('hidden', null);
    }

    // text typed into the reader's quick reply before "open in full editor"
    function restoreQuickReply() {
        var text = '';
        try {
            text = window.sessionStorage.getItem('pb_qr_text') || '';
            window.sessionStorage.removeItem('pb_qr_text');
        } catch (e) {}
        if (!text) {
            return;
        }
        var put = function () {
            if (isHtml() && rc.editor.editor) {
                var p = rc.editor.editor.getDoc().createElement('p');
                p.textContent = text;
                rc.editor.editor.getBody().insertBefore(p, rc.editor.editor.getBody().firstChild);
            } else {
                var ta = $('#composebody');
                ta.val(text + '\n\n' + ta.val());
            }
        };
        if (isHtml() && !(rc.editor && rc.editor.editor && rc.editor.editor.initialized)) {
            rc.addEventListener('editor-load', function () { setTimeout(put, 50); });
        } else {
            put();
        }
    }

    function setupAutocompleteAvatars() {
        if (!window.MutationObserver) {
            return;
        }
        new MutationObserver(function () {
            $('#rcmKSearchpane li').not('.pb-ac').each(function () {
                var li = $(this).addClass('pb-ac');
                var t = li.text(), m = t.match(/[^\s<>"]+@[^\s<>"]+/);
                li.prepend(avatar(t.replace(/<.*$/, ''), m ? m[0] : t, 'sm'));
            });
        }).observe(document.body, { childList: true, subtree: true });
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
            $('body').toggleClass('pb-reading', showing);
        };
        frame.on('load', sync);
        rc.addEventListener('selectfolder', function () { setTimeout(sync, 50); });
        setInterval(sync, 600);
        sync();
    }

    // ------------------------------------------------------- sidebar account row
    function setupAccount() {
        var head = $('#layout-sidebar > .header.pb-account');
        if (!head.length || head.find('.pb-avatar').length) {
            return;
        }
        var user = $.trim(head.find('.header-title').text());
        head.find('.header-title').before(avatar(user, user, 'sm'));
    }

    // contacts list: initials avatars
    function setupContactAvatars() {
        if (rc.env.task != 'addressbook' || !window.MutationObserver) {
            return;
        }
        var paint = function () {
            $('#contacts-table tr td.name').not('.pb-ac').each(function () {
                var td = $(this).addClass('pb-ac');
                var t = $.trim(td.text());
                if (t) {
                    td.prepend(avatar(t, t, 'sm'));
                }
            });
        };
        var tbl = document.getElementById('contacts-table');
        if (tbl) {
            new MutationObserver(paint).observe(tbl, { childList: true, subtree: true });
            paint();
        }
    }

    // ------------------------------------------------------------- palette hint
    function setupTopHint() {
        if (rc.is_framed() || rc.env.action == 'compose') {
            return;
        }
        var btn = $('<button type="button" id="pb-palette-btn">')
            .attr('title', L('palette') + ' (' + (isMac ? '⌘K' : 'Ctrl K') + ')')
            .append('<span class="pb-ico pb-ico-search"></span>')
            .append($('<span class="pb-palette-btn-label">').text(L('palette')))
            .append($('<kbd>').text(isMac ? '⌘K' : 'Ctrl K'))
            .on('click', openPalette);
        $('#layout-menu .popover-header').after(btn);
    }

    setupEditorConfig();

    rc.addEventListener('init', function () {
        if (rc.env.pb_avatars) {
            rc.addEventListener('insertrow', function (evt) { decorateRow(evt.row); });
            if (rc.message_list) {
                $.each(rc.message_list.rows, function (uid, row) { decorateRow(row); });
            }
        }
        if (!rc.is_framed()) {
            setupHoverActions();
            setupShortcuts();
            setupFilters();
            setupSnippets();
            setupPager();
            setupEmptyState();
            setupTopHint();
            setupAccount();
        } else {
            setupShortcuts();
        }
        if (rc.env.action == 'preview' || rc.env.action == 'show') {
            setupReader();
            setupQuickReply();
        }
        setupContactAvatars();
        setupComposer();
        if (rc.env.action == 'compose') {
            setupUndoSend();
        }
    });
})();
