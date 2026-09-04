// Vanilla, dependency-free copy-to-clipboard for DNS records. Delegated on document so
// it keeps working after htmx swaps content in (e.g. re-rendering the DNS Analysis
// panel after "Verify DNS now") without needing to re-bind listeners.
document.addEventListener("click", function (e) {
  const btn = e.target.closest("[data-copy]");
  if (!btn) return;

  const text = btn.getAttribute("data-copy");
  if (!text) return;

  navigator.clipboard.writeText(text).then(function () {
    btn.classList.add("copied");
    // Only swap a dedicated label span's text, never the button's full content —
    // buttons that also contain an icon <svg> would otherwise lose it permanently on
    // the first click (textContent replacement removes child elements, not just text).
    const label = btn.querySelector(".copy-label-text");
    const original = label ? label.textContent : null;
    if (label) label.textContent = "Copied";

    setTimeout(function () {
      btn.classList.remove("copied");
      if (label && original !== null) label.textContent = original;
    }, 1500);
  });
});
