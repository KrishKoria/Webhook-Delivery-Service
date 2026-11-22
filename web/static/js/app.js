// Admin console behaviour. The CSP forbids inline scripts and eval, so every
// interaction that needs JavaScript lives here. Pages work without it: forms
// post normally, times show in UTC, and nothing polls.
(() => {
  "use strict";

  const PAUSE_KEY = "whd.live.paused";
  const store = {
    get(key) { try { return window.localStorage.getItem(key); } catch { return null; } },
    set(key, value) { try { window.localStorage.setItem(key, value); } catch { /* private mode */ } },
  };

  // ---------- live polling ----------
  // Polled regions carry data-poll. Requests are skipped while the tab is
  // hidden or the viewer paused live updates; the region keeps its last state.
  let paused = store.get(PAUSE_KEY) === "1";

  function renderLiveState() {
    document.querySelectorAll("[data-live]").forEach((el) => {
      el.toggleAttribute("data-paused", paused);
      const state = el.querySelector("[data-live-state]");
      if (state) state.textContent = paused ? "Paused" : "Live";
      const btn = el.querySelector("[data-live-toggle]");
      if (btn) {
        btn.textContent = paused ? "Resume" : "Pause";
        btn.setAttribute("aria-pressed", String(paused));
      }
    });
  }

  document.addEventListener("htmx:before:request", (evt) => {
    const el = evt.target;
    if (el instanceof Element && el.hasAttribute("data-poll") && (paused || document.hidden)) {
      evt.preventDefault();
    }
  });

  // ---------- times ----------
  // Server renders <time datetime="RFC 3339">UTC label</time>; show local time
  // in the tooltip and a relative label in the text.
  const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  const dtf = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "medium" });
  const units = [
    ["year", 31536000], ["month", 2592000], ["week", 604800],
    ["day", 86400], ["hour", 3600], ["minute", 60], ["second", 1],
  ];

  function relative(date) {
    const diff = (date.getTime() - Date.now()) / 1000;
    const abs = Math.abs(diff);
    if (abs < 5) return "just now";
    for (const [unit, secs] of units) {
      if (abs >= secs || unit === "second") return rtf.format(Math.round(diff / secs), unit);
    }
    return "";
  }

  function upgradeTimes(root) {
    root.querySelectorAll("time[datetime]").forEach((el) => {
      const date = new Date(el.getAttribute("datetime"));
      if (Number.isNaN(date.getTime())) return;
      if (!el.dataset.utc) el.dataset.utc = el.textContent.trim();
      el.textContent = relative(date);
      el.title = `${dtf.format(date)} (${el.dataset.utc})`;
    });
  }

  // ---------- toasts ----------
  const flash = () => document.getElementById("flash");

  function showToast(kind, msg) {
    const region = flash();
    if (!region) return;
    const toast = document.createElement("div");
    toast.className = `toast toast--${kind}`;
    toast.setAttribute("role", kind === "error" ? "alert" : "status");
    const p = document.createElement("p");
    p.className = "toast__msg";
    p.textContent = msg;
    const close = document.createElement("button");
    close.type = "button";
    close.className = "toast__close";
    close.setAttribute("data-dismiss", "");
    close.setAttribute("aria-label", "Dismiss");
    close.textContent = "×";
    toast.append(p, close);
    region.append(toast);
    scheduleDismiss(toast);
  }

  function scheduleDismiss(toast) {
    if (toast.dataset.timer || toast.classList.contains("toast--error")) return;
    toast.dataset.timer = "1";
    window.setTimeout(() => toast.remove(), 6000);
  }

  const statusMessages = {
    401: "Your session expired. Reload the page to sign in again.",
    403: "The request was blocked as cross-origin. Reload the page and try again.",
    413: "That request is larger than the server accepts.",
    415: "The server rejected the request format.",
    429: "Too many requests. Wait a moment and try again.",
  };

  // Handler errors arrive with their own out-of-band toast. Middleware errors
  // (auth, CSRF, size, rate limit) are JSON, which htmx does not swap (see the
  // noSwap config), so explain them here.
  document.addEventListener("htmx:response:error", (evt) => {
    const ctx = evt.detail && evt.detail.ctx;
    const status = ctx && ctx.response ? (ctx.response.status ?? ctx.response.raw?.status) : 0;
    const body = (ctx && ctx.text) || "";
    if (body.includes("hx-swap-oob")) return;
    if (status === 409 || status === 422) return; // swapped content explains itself
    showToast("error", statusMessages[status] || `Something went wrong (HTTP ${status || "error"}). Try again.`);
  });

  document.addEventListener("htmx:error", (evt) => {
    const ctx = evt.detail && evt.detail.ctx;
    if (ctx && ctx.sourceElement && ctx.sourceElement.hasAttribute("data-poll")) return; // next poll retries
    showToast("error", "Could not reach the server. Check your connection and try again.");
  });

  // ---------- selection (dead letter bulk actions) ----------
  function syncSelection(form) {
    if (!form) return;
    const items = [...form.querySelectorAll("[data-select-item]")];
    const checked = items.filter((i) => i.checked).length;
    const all = form.querySelector("[data-select-all]");
    if (all) {
      all.checked = items.length > 0 && checked === items.length;
      all.indeterminate = checked > 0 && checked < items.length;
    }
    const count = form.querySelector("[data-selected-count]");
    if (count) count.textContent = checked ? `${checked} selected` : "";
    form.querySelectorAll("[data-bulk]").forEach((b) => { b.disabled = checked === 0; });
  }

  document.addEventListener("change", (evt) => {
    const t = evt.target;
    if (!(t instanceof HTMLInputElement)) return;
    if (t.matches("[data-select-all]")) {
      t.form?.querySelectorAll("[data-select-item]").forEach((i) => { i.checked = t.checked; });
      syncSelection(t.form);
    } else if (t.matches("[data-select-item]")) {
      syncSelection(t.form);
    }
  });

  // ---------- confirmations for plain form posts ----------
  // htmx requests use hx-confirm; these are full-page posts (bulk delete,
  // delete from a detail page).
  document.addEventListener("submit", (evt) => {
    const submitter = evt.submitter;
    const msg = submitter?.dataset.confirm || evt.target.dataset?.confirm;
    if (msg && !window.confirm(msg)) evt.preventDefault();
  });

  // ---------- clicks ----------
  document.addEventListener("click", (evt) => {
    const t = evt.target instanceof Element ? evt.target : null;
    if (!t) return;
    const dismiss = t.closest("[data-dismiss]");
    if (dismiss) {
      dismiss.closest(".toast")?.remove();
      return;
    }
    const toggle = t.closest("[data-live-toggle]");
    if (toggle) {
      paused = !paused;
      store.set(PAUSE_KEY, paused ? "1" : "0");
      renderLiveState();
      return;
    }
    // Close open action menus when clicking elsewhere.
    document.querySelectorAll("details.menu[open]").forEach((d) => {
      if (!d.contains(t)) d.removeAttribute("open");
    });
  });

  document.addEventListener("keydown", (evt) => {
    if (evt.key !== "Escape") return;
    document.querySelectorAll("details.menu[open]").forEach((d) => {
      d.removeAttribute("open");
      d.querySelector("summary")?.focus();
    });
  });

  // ---------- scheduling in local time ----------
  // The form posts a datetime-local value plus getTimezoneOffset() for that
  // moment (DST-aware); without JS the server reads the value as UTC.
  function syncSchedule(form) {
    const input = form.querySelector('input[type="datetime-local"]');
    const offset = form.querySelector("[data-tz-offset]");
    const hint = form.querySelector("[data-tz-hint]");
    if (!input || !offset) return;
    const when = input.value ? new Date(input.value) : new Date();
    if (!Number.isNaN(when.getTime())) offset.value = String(when.getTimezoneOffset());
    if (hint) {
      const zone = Intl.DateTimeFormat().resolvedOptions().timeZone || "your local time zone";
      hint.textContent = `In your time zone (${zone}).`;
    }
  }

  function initSchedule(root) {
    root.querySelectorAll("form[data-schedule-form]").forEach((form) => {
      const input = form.querySelector('input[type="datetime-local"]');
      if (input && !form.dataset.tzInit) {
        form.dataset.tzInit = "1";
        // The server pre-fills an hour from now in UTC; convert it to local.
        if (input.value && !form.querySelector("[aria-invalid]")) {
          const utc = new Date(`${input.value}:00Z`);
          if (!Number.isNaN(utc.getTime())) input.value = toLocalInput(utc);
        }
        input.min = toLocalInput(new Date());
      }
      syncSchedule(form);
    });
  }

  function toLocalInput(d) {
    const pad = (n) => String(n).padStart(2, "0");
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  }

  document.addEventListener("input", (evt) => {
    const form = evt.target instanceof Element ? evt.target.closest("form[data-schedule-form]") : null;
    if (form) syncSchedule(form);
  });

  // ---------- lifecycle ----------
  function init(root) {
    upgradeTimes(root);
    initSchedule(root);
    root.querySelectorAll?.(".toast").forEach(scheduleDismiss);
    document.querySelectorAll("form#dlq-bulk").forEach(syncSelection);
  }

  document.addEventListener("htmx:after:swap", (evt) => {
    const target = evt.target instanceof Element ? evt.target : document;
    init(target.isConnected ? target : document);
    init(document.getElementById("flash") || document);
    // A re-rendered form with errors: move focus to the summary.
    const summary = document.querySelector('#form-errors[role="alert"]');
    if (summary && document.activeElement && summary.closest("form")?.contains(document.activeElement)) {
      summary.focus();
    }
    renderLiveState();
  });

  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) upgradeTimes(document);
  });

  function boot() {
    renderLiveState();
    init(document);
    const summary = document.querySelector('#form-errors[role="alert"]');
    if (summary) summary.focus();
    window.setInterval(() => upgradeTimes(document), 30000);
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", boot);
  else boot();
})();
