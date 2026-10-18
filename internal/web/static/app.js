// Progressive enhancements. Every page works without this script; it only adds
// conveniences on top of the server-rendered HTML.
(function () {
  "use strict";

  const header = () => document.querySelector(".site-header");
  const headerHeight = () => (header() ? header().getBoundingClientRect().height : 0);
  const narrow = window.matchMedia("(max-width: 1099px)");

  // --- Reader ----------------------------------------------------------------

  /** The paragraph or section title at the top of the viewport. */
  function topParagraph() {
    const top = headerHeight() + 8;
    for (const el of document.querySelectorAll("[data-book-text] > [id^='p-']")) {
      if (el.getBoundingClientRect().bottom > top) return el;
    }
    return null;
  }

  function initReader() {
    const text = document.querySelector("[data-book-text]");
    if (!text) return;

    // Legacy links carry the position as ?pos=<index>:<offset in px>.
    const params = new URLSearchParams(location.search);
    const pos = params.get("pos");
    if (pos && !location.hash) {
      const [index, offset] = pos.split(":").map((n) => parseInt(n, 10));
      const target = document.getElementById("p-" + index);
      if (target) {
        const y = target.getBoundingClientRect().top + window.scrollY - (offset || headerHeight() + 16);
        window.scrollTo({ top: Math.max(y, 0) });
        target.classList.add("is-target");
      }
    }

    // Switching the language keeps the reading position: paragraph indexes are
    // the same in every language version of a book.
    document.addEventListener("click", (e) => {
      const link = e.target.closest("[data-lang-link]");
      if (!link) return;
      const p = topParagraph();
      if (p && window.scrollY > 0) {
        link.href = link.href.split("#")[0] + "#" + p.id;
      }
    });

    initTOC();
    initToTop();
  }

  function initTOC() {
    const panel = document.querySelector("[data-toc]");
    if (!panel) return;
    // Open beside the text on wide screens, collapsed above it on narrow ones.
    if (narrow.matches) panel.open = false;
    narrow.addEventListener("change", (e) => {
      panel.open = !e.matches;
    });

    panel.addEventListener("click", (e) => {
      if (e.target.closest("[data-toc-link]") && narrow.matches) panel.open = false;
    });

    // Highlight the section being read.
    const links = new Map();
    panel.querySelectorAll("[data-toc-link]").forEach((a) => links.set("p-" + a.dataset.tocLink, a));
    const titles = [...document.querySelectorAll("[data-section]")];
    if (!titles.length || !("IntersectionObserver" in window)) return;

    let current = null;
    const update = () => {
      const limit = headerHeight() + window.innerHeight * 0.25;
      let active = null;
      for (const t of titles) {
        if (t.getBoundingClientRect().top <= limit) active = t;
        else break;
      }
      const link = active ? links.get(active.id) : null;
      if (link === current) return;
      if (current) current.removeAttribute("aria-current");
      current = link;
      if (link) {
        link.setAttribute("aria-current", "location");
        if (!narrow.matches) link.scrollIntoView({ block: "nearest" });
      }
    };
    let scheduled = false;
    window.addEventListener("scroll", () => {
      if (scheduled) return;
      scheduled = true;
      requestAnimationFrame(() => {
        scheduled = false;
        update();
      });
    }, { passive: true });
    update();
  }

  function initToTop() {
    const button = document.querySelector("[data-to-top]");
    if (!button) return;
    const toggle = () => button.classList.toggle("is-visible", window.scrollY > window.innerHeight);
    window.addEventListener("scroll", toggle, { passive: true });
    toggle();
    button.addEventListener("click", (e) => {
      e.preventDefault();
      window.scrollTo({ top: 0, behavior: "smooth" });
      history.replaceState(null, "", location.pathname + location.search);
    });
  }

  // --- Global behaviors (delegated, so they also work in htmx-swapped content) --

  document.addEventListener("click", async (e) => {
    const copy = e.target.closest("[data-copy]");
    if (copy) {
      try {
        await navigator.clipboard.writeText(copy.dataset.copy);
        copy.classList.add("is-done");
        setTimeout(() => copy.classList.remove("is-done"), 1500);
      } catch (_) {
        /* clipboard unavailable */
      }
      return;
    }

    const swap = e.target.closest("[data-swap-languages]");
    if (swap) {
      const form = swap.closest("form");
      const from = form.querySelector("[data-lang-from]");
      const to = form.querySelector("[data-lang-to]");
      [from.value, to.value] = [to.value, from.value];
      from.dispatchEvent(new Event("change", { bubbles: true }));
      return;
    }

    // Close open dropdown menus when clicking elsewhere.
    document.querySelectorAll("details[data-dismissable][open]").forEach((d) => {
      if (!d.contains(e.target)) d.open = false;
    });
  });

  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      document.querySelectorAll("details[data-dismissable][open]").forEach((d) => {
        d.open = false;
        d.querySelector("summary").focus();
      });
    }
    // "/" focuses the search box, as on many reading sites.
    if (e.key === "/" && !e.target.closest("input, textarea, select, [contenteditable]")) {
      const search = document.querySelector(".searchbar__input");
      if (search) {
        e.preventDefault();
        search.focus();
      }
    }
  });

  // Keep the textarea language in sync with the selected source language.
  document.addEventListener("change", (e) => {
    if (e.target.matches("[data-lang-from]")) {
      const source = e.target.form.querySelector("textarea[name=text]");
      if (source) source.lang = e.target.value;
    }
  });

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", initReader);
  } else {
    initReader();
  }
})();
