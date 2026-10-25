// patrabahok admin: small, dependency-free behaviors shared by every page. Everything
// is delegated on document, so it keeps working after htmx swaps content in.
(function () {
  "use strict";

  // ---- theme ------------------------------------------------------------------
  var THEME_KEY = "patrabahok_theme";
  var root = document.documentElement;
  function isDark() {
    var t = root.getAttribute("data-theme");
    if (t) return t === "dark";
    return window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches;
  }
  document.addEventListener("click", function (e) {
    if (!e.target.closest("#theme-toggle")) return;
    var next = isDark() ? "light" : "dark";
    root.setAttribute("data-theme", next);
    try { localStorage.setItem(THEME_KEY, next); } catch (err) {}
  });

  // ---- mobile navigation -------------------------------------------------------
  document.addEventListener("click", function (e) {
    var app = document.querySelector(".app");
    if (!app) return;
    if (e.target.closest("[data-nav-toggle]")) app.classList.toggle("nav-open");
    else if (e.target.closest(".sidebar-backdrop")) app.classList.remove("nav-open");
  });

  // ---- toasts -----------------------------------------------------------------
  var ICONS = {
    success: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>',
    error: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"><path d="M12 8v5M12 16.5h.01"/></svg>'
  };
  function toast(kind, message) {
    var host = document.getElementById("toasts");
    if (!host || !message) return;
    var el = document.createElement("div");
    el.className = "toast " + (kind === "error" ? "error" : "success");
    el.setAttribute("role", kind === "error" ? "alert" : "status");
    var icon = document.createElement("span");
    icon.className = "toast-icon";
    icon.innerHTML = ICONS[kind === "error" ? "error" : "success"];
    var msg = document.createElement("div");
    msg.className = "toast-msg";
    msg.textContent = message;
    el.appendChild(icon);
    el.appendChild(msg);
    host.appendChild(el);
    setTimeout(function () {
      el.classList.add("leaving");
      setTimeout(function () { el.remove(); }, 250);
    }, kind === "error" ? 7000 : 4000);
  }
  window.pbToast = toast;

  document.addEventListener("DOMContentLoaded", function () {
    var f = document.getElementById("flash-data");
    if (f) toast(f.getAttribute("data-kind"), f.getAttribute("data-message"));
  });
  document.addEventListener("toast", function (e) {
    if (e.detail) toast(e.detail.kind, e.detail.message);
  });
  document.addEventListener("htmx:responseError", function (e) {
    var xhr = e.detail.xhr;
    var text = (xhr && xhr.responseText || "").trim();
    if (!text || text.charAt(0) === "<") text = "Request failed (HTTP " + (xhr ? xhr.status : "?") + ").";
    toast("error", text.length > 400 ? text.slice(0, 400) + "…" : text);
  });
  document.addEventListener("htmx:sendError", function () {
    toast("error", "Couldn't reach the server. Check your connection and try again.");
  });

  // ---- modals -----------------------------------------------------------------
  document.addEventListener("click", function (e) {
    var opener = e.target.closest("[data-open]");
    if (opener) {
      var d = document.getElementById(opener.getAttribute("data-open"));
      if (d && d.showModal) {
        e.preventDefault();
        d.showModal();
        var first = d.querySelector("input:not([type=hidden]),select,textarea");
        if (first) first.focus();
      }
      return;
    }
    if (e.target.closest("[data-close]")) {
      var dlg = e.target.closest("dialog");
      if (dlg) dlg.close();
      return;
    }
    // Click on the backdrop (the dialog element itself, outside its content) closes it.
    if (e.target.tagName === "DIALOG" && e.target.classList.contains("modal")) e.target.close();
  });

  // ---- styled confirmation for hx-confirm ---------------------------------------
  document.addEventListener("htmx:confirm", function (e) {
    var question = e.detail.question;
    if (!question) return;
    e.preventDefault();
    var d = document.getElementById("confirm-dialog");
    if (!d) {
      if (window.confirm(question)) e.detail.issueRequest(true);
      return;
    }
    d.querySelector("[data-confirm-text]").textContent = question;
    var danger = /delete|remove|revoke|suspend|rotate|disable/i.test(question);
    var ok = d.querySelector("[data-confirm-ok]");
    ok.className = "btn " + (danger ? "btn-danger-solid" : "btn-primary");
    var handler = function () {
      d.close();
      e.detail.issueRequest(true);
    };
    ok.onclick = handler;
    d.showModal();
    ok.focus();
  });

  // ---- log views start scrolled to the newest line ------------------------------
  function scrollLogs(root) {
    (root || document).querySelectorAll(".log-view").forEach(function (el) { el.scrollTop = el.scrollHeight; });
  }
  document.addEventListener("DOMContentLoaded", function () { scrollLogs(); });
  document.addEventListener("htmx:afterSwap", function (e) { scrollLogs(e.detail.target.parentNode || document); });

  // ---- keyboard ---------------------------------------------------------------
  document.addEventListener("keydown", function (e) {
    if (e.key !== "/" || e.ctrlKey || e.metaKey) return;
    var t = e.target;
    if (t.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(t.tagName)) return;
    var s = document.getElementById("global-search");
    if (s) { e.preventDefault(); s.focus(); }
  });

  // ---- clickable table rows -----------------------------------------------------
  document.addEventListener("click", function (e) {
    var row = e.target.closest("tr[data-href]");
    if (!row || e.target.closest("a,button,input,select,label,form,.dropdown")) return;
    window.location = row.getAttribute("data-href");
  });

  // ---- password generator ---------------------------------------------------------
  document.addEventListener("click", function (e) {
    var btn = e.target.closest("[data-generate-password]");
    if (!btn) return;
    var input = document.getElementById(btn.getAttribute("data-generate-password"));
    if (!input) return;
    var alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
    var bytes = new Uint32Array(14);
    window.crypto.getRandomValues(bytes);
    var out = "";
    for (var i = 0; i < bytes.length; i++) out += alphabet[bytes[i] % alphabet.length];
    input.type = "text";
    input.value = out;
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });

  // ---- download a <textarea> or data attribute as a file --------------------------
  document.addEventListener("click", function (e) {
    var btn = e.target.closest("[data-download]");
    if (!btn) return;
    var src = document.getElementById(btn.getAttribute("data-download"));
    if (!src) return;
    var blob = new Blob([src.value || src.textContent], { type: "text/csv;charset=utf-8" });
    var a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = btn.getAttribute("data-filename") || "download.csv";
    document.body.appendChild(a);
    a.click();
    setTimeout(function () { URL.revokeObjectURL(a.href); a.remove(); }, 0);
  });
})();
