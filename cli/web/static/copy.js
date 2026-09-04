// Vanilla, dependency-free copy-to-clipboard for DNS records. Delegated on document so
// it keeps working after htmx swaps content in (e.g. re-rendering the DNS Analysis
// panel after "Verify DNS now") without needing to re-bind listeners.
document.addEventListener("click", function (e) {
  const btn = e.target.closest("[data-copy]");
  if (!btn) return;

  const text = btn.getAttribute("data-copy");
  if (!text) return;

  navigator.clipboard.writeText(text).then(function () {
    const original = btn.getAttribute("data-copy-label") || btn.textContent;
    btn.classList.add("copied");
    if (btn.hasAttribute("data-copy-label")) {
      btn.textContent = "Copied";
    }
    setTimeout(function () {
      btn.classList.remove("copied");
      if (btn.hasAttribute("data-copy-label")) {
        btn.textContent = original;
      }
    }, 1500);
  });
});
