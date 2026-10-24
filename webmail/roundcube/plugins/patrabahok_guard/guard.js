/**
 * patrabahok_guard client: risky-link confirmation, banner dismiss, attachment scan
 * badges (message view) and inbox categorization helpers (mail list).
 *
 * @license GNU GPLv3+
 */
(function () {
    'use strict';

    if (!window.rcmail) {
        return;
    }

    var rc = window.rcmail;

    function label(name, vars) {
        var s = rc.get_label('patrabahok_guard.' + name);
        if (vars) {
            $.each(vars, function (k, v) {
                s = s.split('$' + k).join(v);
            });
        }
        return s;
    }

    function esc(s) {
        return $('<div>').text(s == null ? '' : String(s)).html();
    }

    // ---------------------------------------------------------------------------------
    // Message view: banner + risky links
    // ---------------------------------------------------------------------------------

    function confirmLink(a) {
        var href = a.getAttribute('href') || '',
            real = a.getAttribute('data-pb-real') || '',
            level = a.getAttribute('data-pb-risk') || 'warn',
            host = rc.is_framed() ? parent : window,
            shown = href.length > 160 ? href.slice(0, 157) + '…' : href,
            html = '<div class="pb-guard-dialog pb-guard-' + esc(level) + '">'
                + '<p>' + esc(label('linkdialogtext')) + '</p>'
                + '<p class="pb-guard-dialog-real">' + esc(label('linkdialogreal'))
                + ' <strong>' + esc(real) + '</strong></p>'
                + '<p class="pb-guard-dialog-url"><code>' + esc(shown) + '</code></p></div>';

        var close = function (el) {
            var jq = el.jqref || host.$ || $;
            jq(el).dialog('close');
        };

        rc.show_popup_dialog(html, label('linkdialogtitle'), [
            {
                text: label('goback'),
                class: 'mainaction pb-guard-goback',
                click: function () {
                    close(this);
                },
            },
            {
                text: label('openanyway'),
                class: 'pb-guard-openanyway',
                click: function () {
                    close(this);
                    // Open from the window hosting the dialog: it has the user activation.
                    var w = host.open(href, '_blank', 'noopener,noreferrer');
                    if (w) {
                        try {
                            w.opener = null;
                        } catch (e) {}
                    }
                },
            },
        ], {
            width: 460,
            open: function () {
                // "Go back" is the default action
                var btn = $(this).parent().find('.pb-guard-goback');
                setTimeout(function () {
                    btn.trigger('focus');
                }, 10);
            },
        });
    }

    function initMessage() {
        // Capture phase: runs before Roundcube's and the skin's link handlers.
        document.addEventListener('click', function (e) {
            var a = e.target && e.target.closest ? e.target.closest('a.pb-link-risk') : null;
            if (a && !e.defaultPrevented) {
                e.preventDefault();
                e.stopImmediatePropagation();
                confirmLink(a);
            }
        }, true);

        // Middle-click / auxclick would open the link without a confirmation.
        document.addEventListener('auxclick', function (e) {
            var a = e.target && e.target.closest ? e.target.closest('a.pb-link-risk') : null;
            if (a) {
                e.preventDefault();
                e.stopImmediatePropagation();
                confirmLink(a);
            }
        }, true);

        $(document).on('click', '.pb-guard-banner .pb-guard-dismiss', function () {
            $(this).closest('.pb-guard-banner').addClass('pb-guard-hidden').attr('hidden', 'hidden');
        });

        var g = rc.env.pb_guard;
        if (g && g.verified) {
            $('html').addClass('pb-sender-verified');
        }

        if (rc.env.pb_scan && rc.env.uid) {
            initScan();
        }
    }

    // ---------------------------------------------------------------------------------
    // Message view: attachment scanning
    // ---------------------------------------------------------------------------------

    var blocked = {};

    function attItems() {
        return $('#attachment-list > li[id^="attach"], ul.attachmentslist > li[id^="attach"]');
    }

    function badge(li, status, title, text) {
        $(li).find('.pb-scan-badge').remove();
        var b = $('<span>').addClass('pb-scan-badge pb-scan-' + status).attr('title', title).text(text);
        $(li).append(b);
    }

    function initScan() {
        var items = attItems();
        if (!items.length) {
            return;
        }

        items.each(function () {
            badge(this, 'pending', label('scanpendingtitle'), label('scanpending'));
        });

        rc.addEventListener('plugin.pbguard_scan_result', onScanResult);
        $.each(['load-attachment', 'open-attachment', 'download-attachment'], function (i, cmd) {
            rc.addEventListener('before' + cmd, function (props) {
                if (props && blocked[props]) {
                    rc.alert_dialog(esc(label('attblocked', { virus: blocked[props] })));
                    return false;
                }
            });
        });

        rc.http_post('plugin.pbguard-scan', { _uid: rc.env.uid, _mbox: rc.env.mailbox });
    }

    function onScanResult(data) {
        if (!data || String(data.uid) !== String(rc.env.uid)) {
            return;
        }

        var parts = data.parts || {};
        attItems().each(function () {
            var id = this.id.replace(/^attach/, ''),
                p = parts[id],
                li = this;

            if (!p) {
                $(li).find('.pb-scan-badge').remove();
                return;
            }

            if (p.status === 'infected') {
                blocked[id] = p.virus || '?';
                badge(li, 'infected', label('scaninfectedtitle', { virus: p.virus || '' }), label('scaninfected'));
                $(li).addClass('pb-att-blocked');
                $(li).find('a').each(function () {
                    $(this).removeAttr('href').removeAttr('onclick').attr({ 'aria-disabled': 'true', tabindex: '-1' })
                        .off('click').on('click', function (e) {
                            e.preventDefault();
                            e.stopImmediatePropagation();
                            rc.alert_dialog(esc(label('attblocked', { virus: blocked[id] })));
                            return false;
                        });
                });
                if (rc.env.attachments) {
                    delete rc.env.attachments[id];
                }
            } else if (p.status === 'clean') {
                badge(li, 'clean', label('scancleantitle'), label('scanclean'));
            } else if (p.status === 'skipped') {
                badge(li, 'error', label('scanskippedtitle'), label('scanskipped'));
            } else {
                badge(li, 'error', label('scanerrortitle'), label('scanerror'));
            }
        });

        rc.triggerEvent('pbguard-scan', data);
    }

    // ---------------------------------------------------------------------------------
    // Mail list: categories
    // ---------------------------------------------------------------------------------

    var catCallbacks = {}, catSeq = 0, catBusy = false, catTimer = null, catLoops = 0;

    function catRequest(action, params, cb) {
        var seq = ++catSeq;
        if (cb) {
            catCallbacks[seq] = cb;
        }
        params._seq = seq;
        rc.http_post(action, params);
    }

    function onCategories(data) {
        var seq = data && data.seq, cb = catCallbacks[seq];
        delete catCallbacks[seq];
        if (data && data.counts) {
            rc.env.pb_category_counts = data.counts;
        }
        rc.triggerEvent('pbguard-categories', data);
        if (typeof cb === 'function') {
            cb(data);
        }
    }

    function categorize(cb) {
        if (!rc.env.pb_categories_enabled) {
            if (cb) {
                cb(null);
            }
            return;
        }

        catBusy = true;
        catRequest('plugin.pbguard-categorize', { _mbox: 'INBOX' }, function (data) {
            catBusy = false;
            if (cb) {
                cb(data);
            }
            // Large inboxes are classified in batches.
            if (data && data.more && catLoops++ < 20) {
                setTimeout(function () {
                    categorize();
                }, 250);
            } else {
                catLoops = 0;
            }
        });
    }

    function setCategory(uids, cat, cb) {
        if (!$.isArray(uids)) {
            uids = String(uids || '').split(',');
        }
        uids = $.grep($.map(uids, function (u) {
            return String(u).replace(/[^0-9]/g, '');
        }), function (u) {
            return u.length > 0;
        });

        if (!uids.length) {
            if (cb) {
                cb(null);
            }
            return;
        }

        catRequest('plugin.pbguard-setcat', { _uid: uids.join(','), _mbox: 'INBOX', _cat: cat }, cb);
    }

    function scheduleCategorize() {
        if (!rc.env.pb_categories_enabled || rc.env.mailbox !== 'INBOX') {
            return;
        }
        clearTimeout(catTimer);
        catTimer = setTimeout(function () {
            if (catBusy) {
                scheduleCategorize();
            } else {
                categorize();
            }
        }, 800);
    }

    function initList() {
        rc.addEventListener('plugin.pbguard_categories', onCategories);
        // A failed request must not block automatic runs forever.
        rc.addEventListener('responseafterplugin.pbguard-categorize', function () {
            catBusy = false;
        });
        rc.addEventListener('listupdate', scheduleCategorize);
        rc.addEventListener('responseaftercheck-recent', scheduleCategorize);
    }

    window.pbGuard = {
        categorize: categorize,
        setCategory: setCategory,
    };

    rc.addEventListener('init', function () {
        if (rc.env.task !== 'mail') {
            return;
        }

        if (rc.env.action === 'show' || rc.env.action === 'preview' || rc.env.action === 'print') {
            initMessage();
        } else if (!rc.env.action || rc.env.action === 'list') {
            if (rc.env.pb_categories_enabled) {
                initList();
            }
        }
    });
})();
