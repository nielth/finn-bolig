// ==UserScript==
// @name         boligjakt crawler
// @namespace    https://github.com/nielth/finn-bolig
// @version      0.2.0
// @description  Crawls Stavanger homes on finn.no once a day from your own browser and sends them to your local boligjakt app.
// @match        https://www.finn.no/*
// @grant        GM_xmlhttpRequest
// @grant        GM_getValue
// @grant        GM_setValue
// @connect      localhost
// @connect      127.0.0.1
// @run-at       document-idle
// @updateURL    http://localhost:8080/finn-bolig.user.js
// @downloadURL  http://localhost:8080/finn-bolig.user.js
// ==/UserScript==

(function () {
  "use strict";

  // All homes for sale in Stavanger kommune (not new-build projects).
  const SEARCH = "location=1.20012.20196&is_new_property=false";
  const MAX_SEARCH_PAGES = 100;

  const backend = () => GM_getValue("backend", "http://localhost:8080");
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  // Slow and polite: 2-4 seconds between requests to finn.
  const politeDelay = () => sleep(2000 + Math.random() * 2000);
  let stopRequested = false;
  let crawling = false;

  // ---------- backend ----------

  function call(method, path, body, contentType = "application/json") {
    return new Promise((resolve, reject) => {
      GM_xmlhttpRequest({
        method,
        url: backend() + path,
        headers: { "Content-Type": contentType },
        data: body == null ? undefined : contentType === "application/json" ? JSON.stringify(body) : body,
        onload: (res) => {
          if (res.status >= 400) {
            let msg = res.responseText;
            try {
              msg = JSON.parse(res.responseText).error;
            } catch {
              // plain text error
            }
            const err = new Error(msg || `backend returned ${res.status}`);
            err.status = res.status;
            return reject(err);
          }
          resolve(res.responseText ? JSON.parse(res.responseText) : null);
        },
        onerror: () => reject(new Error(`could not reach ${backend()}, is the app running?`)),
      });
    });
  }

  // ---------- finn ----------

  async function finnGet(url) {
    const res = await fetch(url, { credentials: "include", headers: { Accept: "*/*" } });
    const text = await res.text();
    // finn's bot protection answers with 403 and a challenge page when it wants a human check.
    if (res.status === 403 && /captcha-delivery|enable JS/i.test(text)) {
      throw new Error("finn wants to check that you are human. Reload any finn.no page, complete the check, and press Crawl now.");
    }
    return { status: res.status, url: res.url, text };
  }

  function searchUrl(page) {
    const params = new URLSearchParams(SEARCH);
    params.set("page", String(page));
    params.set("_routes", "routes/realestate/search/search");
    return `https://www.finn.no/realestate/homes/search.html.data?${params}`;
  }

  // ---------- crawl ----------

  async function crawl() {
    if (crawling) return;
    crawling = true;
    stopRequested = false;
    let crawlId = null;
    try {
      ({ id: crawlId } = await call("POST", "/api/crawl/start"));

      // 1. All search result pages.
      let total = 0;
      let complete = false;
      for (let page = 1; page <= MAX_SEARCH_PAGES && !stopRequested; page++) {
        status(`Search page ${page} (${total} homes so far)…`);
        const res = await finnGet(searchUrl(page));
        if (res.status !== 200) throw new Error(`finn returned ${res.status} for search page ${page}`);
        const r = await call("POST", `/api/crawl/${crawlId}/search`, res.text, "text/plain");
        total += r.found;
        if (r.found === 0 || (r.last_page && page >= r.last_page)) {
          complete = true;
          break;
        }
        await politeDelay();
      }
      if (complete && total === 0) throw new Error("Found no homes in the search results; finn may have changed its format.");
      await call("POST", `/api/crawl/${crawlId}/search-done`, { complete });

      // 2. Ad pages for new, changed, stale and disappeared ads.
      const { finnkodes } = await call("GET", `/api/crawl/${crawlId}/todo`);
      for (let i = 0; i < finnkodes.length && !stopRequested; i++) {
        const id = finnkodes[i];
        status(`Ad ${i + 1} of ${finnkodes.length} (finnkode ${id})…`);
        await politeDelay();
        try {
          const res = await finnGet(`https://www.finn.no/realestate/homes/ad.html?finnkode=${id}`);
          const q = new URLSearchParams({ status: res.status, url: res.url });
          if (res.status === 200 || res.status === 404 || res.status === 410) {
            await call("POST", `/api/crawl/${crawlId}/ad/${id}?${q}`, res.status === 200 ? res.text : "", "text/plain");
          } else {
            await call("POST", `/api/crawl/${crawlId}/failed/${id}`);
          }
        } catch (e) {
          if (/human|reach/.test(e.message)) throw e;
          console.warn("[boligjakt]", id, e);
          await call("POST", `/api/crawl/${crawlId}/failed/${id}`).catch(() => {});
        }
      }

      const done = await call("POST", `/api/crawl/${crawlId}/finish`, { error: stopRequested ? "stopped" : "" });
      status(
        stopRequested
          ? "Stopped."
          : `Done: ${done.found} homes, ${done.new_ads} new, ${done.updated_ads} updated, ${done.sold_ads} sold, ${done.removed_ads} removed.`,
      );
    } catch (e) {
      console.error("[boligjakt]", e);
      status(e.status === 409 ? "Another tab is already crawling." : `Error: ${e.message}`);
      if (crawlId) await call("POST", `/api/crawl/${crawlId}/finish`, { error: e.message }).catch(() => {});
    } finally {
      crawling = false;
    }
  }

  // Start the daily crawl automatically when it is due and no other tab is crawling.
  async function maybeAutoCrawl() {
    try {
      const s = await call("GET", "/api/crawl/status");
      if (s.running) {
        status(crawling ? statusText : "Crawling in another tab.");
      } else if (s.due) {
        status("Daily crawl starting in 10 seconds…");
        await sleep(10000);
        await crawl();
      } else if (!crawling) {
        status(s.last ? `Last crawl ${new Date(s.last.finished_at).toLocaleString()}.` : "");
      }
    } catch (e) {
      status(`Error: ${e.message}`);
    }
  }

  // ---------- the ad you are looking at ----------

  function currentFinnkode() {
    const m = location.search.match(/finnkode=(\d+)/) || location.pathname.match(/\/(\d{6,})(?:\/|$)/);
    return m ? Number(m[1]) : null;
  }

  async function saveCurrentAd() {
    const id = currentFinnkode();
    if (!id) return null;
    const q = new URLSearchParams({ url: location.href });
    return call("POST", `/api/listings/${id}/page?${q}`, document.documentElement.outerHTML, "text/plain");
  }

  // ---------- panel ----------

  let statusEl;
  let statusText = "";
  function status(text) {
    statusText = text;
    if (statusEl) statusEl.textContent = text;
  }

  function panel() {
    const box = document.createElement("div");
    box.style.cssText =
      "position:fixed;right:16px;bottom:16px;z-index:2147483647;background:#fff;border:1px solid #cbd5e1;" +
      "border-radius:10px;box-shadow:0 4px 16px rgba(0,0,0,.15);padding:10px 12px;font:13px system-ui,sans-serif;" +
      "color:#1b1b1f;width:250px;display:flex;flex-direction:column;gap:6px";
    const row = document.createElement("div");
    row.style.cssText = "display:flex;gap:6px;align-items:center";
    const title = document.createElement("b");
    title.textContent = "boligjakt";
    title.style.cssText = "color:#0063fb;margin-right:auto";
    row.appendChild(title);

    const button = (text, onclick) => {
      const b = document.createElement("button");
      b.textContent = text;
      b.style.cssText = "padding:3px 8px;border-radius:6px;cursor:pointer;font:inherit;background:#fff;border:1px solid #cbd5e1;color:#1b1b1f";
      b.onclick = onclick;
      row.appendChild(b);
      return b;
    };
    if (currentFinnkode()) {
      button("★", async () => {
        try {
          await saveCurrentAd();
          await call("POST", "/api/references", { input: String(currentFinnkode()) });
          status("Added to favourites.");
        } catch (e) {
          status(`Error: ${e.message}`);
        }
      }).title = "Add this home to your favourites";
    }
    button("Crawl now", () => crawl());
    button("Stop", () => {
      stopRequested = true;
      status("Stopping after the current request…");
    });
    box.appendChild(row);

    statusEl = document.createElement("div");
    statusEl.style.cssText = "color:#64748b;font-size:12px;min-height:16px";
    box.appendChild(statusEl);

    const settings = document.createElement("a");
    settings.textContent = "Backend: " + backend();
    settings.href = "#";
    settings.style.cssText = "font-size:11px;color:#94a3b8";
    settings.onclick = (e) => {
      e.preventDefault();
      const v = prompt("boligjakt backend URL", backend());
      if (v) {
        GM_setValue("backend", v.replace(/\/$/, ""));
        settings.textContent = "Backend: " + backend();
      }
    };
    box.appendChild(settings);
    document.body.appendChild(box);
  }

  panel();

  // Every ad you open on finn is saved too (this is how favourites outside Stavanger get their data).
  if (currentFinnkode()) {
    saveCurrentAd()
      .then((r) => r && status(`Saved this home (${r.images} photos).`))
      .catch((e) => status(`Error: ${e.message}`));
  }

  maybeAutoCrawl();
  // Check again every 30 minutes for tabs that stay open.
  setInterval(maybeAutoCrawl, 30 * 60 * 1000);
})();
