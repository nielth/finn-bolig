// ==UserScript==
// @name         boligjakt sync
// @namespace    https://github.com/nielth/finn-bolig
// @version      0.1.1
// @description  Send homes from finn.no to your local boligjakt app, at a slow and polite pace.
// @updateURL    http://localhost:8080/finn-bolig.user.js
// @downloadURL  http://localhost:8080/finn-bolig.user.js
// @match        https://www.finn.no/*
// @grant        GM_xmlhttpRequest
// @grant        GM_getValue
// @grant        GM_setValue
// @connect      localhost
// @connect      127.0.0.1
// @run-at       document-idle
// ==/UserScript==

(function () {
  "use strict";

  const DEFAULT_SEARCH = "location=1.20012.20196&is_new_property=false";
  // Stavanger (mainland, Hundvåg and the nearest islands): west, south, east, north.
  const SOLD_BBOX = "5.60,58.88,5.82,59.02";
  const MAX_SEARCH_PAGES = 60;

  const backend = () => GM_getValue("backend", "http://localhost:8080");
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  const politeDelay = () => sleep(1500 + Math.random() * 1500);
  let stopRequested = false;

  // ---------- backend ----------

  function call(method, path, body) {
    return new Promise((resolve, reject) => {
      GM_xmlhttpRequest({
        method,
        url: backend() + path,
        headers: { "Content-Type": "application/json" },
        data: body ? JSON.stringify(body) : undefined,
        onload: (res) => {
          if (res.status >= 400) return reject(new Error(`backend ${res.status}: ${res.responseText}`));
          resolve(res.responseText ? JSON.parse(res.responseText) : null);
        },
        onerror: () => reject(new Error(`could not reach ${backend()}, is the app running?`)),
      });
    });
  }

  // ---------- turbo-stream (React Router single fetch) decoding ----------

  const SPECIAL = { "-1": undefined, "-2": NaN, "-3": -Infinity, "-4": -0, "-5": null, "-6": Infinity, "-7": undefined };

  function decodeTurboStream(text) {
    const lines = text.split("\n").filter((l) => l.trim());
    let values;
    try {
      values = JSON.parse(lines[0]);
    } catch {
      return null;
    }
    if (!Array.isArray(values)) return values; // plain JSON
    const promises = {};
    for (const line of lines.slice(1)) {
      const m = line.match(/^P(\d+):(.*)$/s);
      if (!m) continue;
      try {
        const chunk = JSON.parse(m[2]);
        if (Array.isArray(chunk) && chunk.length === 1 && Array.isArray(chunk[0]) && chunk[0][0] === "Z") {
          promises[m[1]] = chunk[0][1];
        } else if (Array.isArray(chunk)) {
          promises[m[1]] = values.length;
          values.push(...chunk);
        }
      } catch {
        // ignore broken chunks
      }
    }
    const memo = new Map();

    function hydrate(index) {
      if (index < 0) return SPECIAL[index];
      if (memo.has(index)) return memo.get(index);
      const v = values[index];
      if (v === null || typeof v !== "object") {
        memo.set(index, v);
        return v;
      }
      if (Array.isArray(v)) {
        if (typeof v[0] === "string") {
          const [tag, a] = v;
          switch (tag) {
            case "D":
              return new Date(a).toISOString();
            case "P":
              return promises[a] !== undefined ? hydrate(promises[a]) : null;
            case "Z":
              return hydrate(a);
            case "U":
            case "B":
            case "Y":
              return a;
            case "N": {
              const obj = {};
              memo.set(index, obj);
              return obj;
            }
            case "M": {
              const obj = {};
              memo.set(index, obj);
              for (let i = 1; i < v.length; i += 2) obj[String(hydrate(v[i]))] = hydrate(v[i + 1]);
              return obj;
            }
            case "S": {
              const arr = [];
              memo.set(index, arr);
              for (let i = 1; i < v.length; i++) arr.push(hydrate(v[i]));
              return arr;
            }
            default:
              return null;
          }
        }
        const arr = [];
        memo.set(index, arr);
        for (const i of v) arr.push(hydrate(i));
        return arr;
      }
      const obj = {};
      memo.set(index, obj);
      for (const [k, i] of Object.entries(v)) {
        const key = k.startsWith("_") ? values[Number(k.slice(1))] : k;
        obj[key] = hydrate(i);
      }
      return obj;
    }
    return hydrate(0);
  }

  // ---------- generic helpers ----------

  function walk(root, visit) {
    const seen = new Set();
    const stack = [root];
    while (stack.length) {
      const node = stack.pop();
      if (!node || typeof node !== "object" || seen.has(node)) continue;
      seen.add(node);
      if (visit(node) === false) continue;
      for (const v of Object.values(node)) if (v && typeof v === "object") stack.push(v);
    }
  }

  function num(v) {
    if (v == null) return null;
    if (typeof v === "number") return Number.isFinite(v) ? Math.round(v) : null;
    if (typeof v === "string") {
      const digits = v.replace(/[^\d]/g, "");
      return digits ? Number(digits) : null;
    }
    if (typeof v === "object") return num(v.amount ?? v.value ?? v.size ?? v.size_from ?? v.from);
    return null;
  }

  function float(v) {
    const n = typeof v === "string" ? parseFloat(v) : v;
    return typeof n === "number" && Number.isFinite(n) ? n : null;
  }

  function str(v) {
    if (typeof v === "string" && v.trim()) return v.trim();
    return null;
  }

  // Finds the first value for any of the keys, searching breadth-first so top-level values win.
  function findKey(root, keys, accept = (v) => v != null) {
    const queue = [root];
    const seen = new Set();
    while (queue.length) {
      const node = queue.shift();
      if (!node || typeof node !== "object" || seen.has(node)) continue;
      seen.add(node);
      for (const k of keys) if (k in node && accept(node[k])) return node[k];
      for (const v of Object.values(node)) if (v && typeof v === "object") queue.push(v);
    }
    return null;
  }

  function stripHtml(s) {
    const div = document.createElement("div");
    div.innerHTML = s;
    return div.textContent.trim();
  }

  // ---------- search results ----------

  function toListing(doc) {
    const id = num(doc.ad_id ?? doc.adId ?? doc.id);
    if (!id) return null;
    const coords = doc.coordinates || {};
    const images = Array.isArray(doc.image_urls) ? doc.image_urls.filter((u) => typeof u === "string") : [];
    if (!images.length && doc.image?.url) images.push(doc.image.url);
    return {
      finnkode: id,
      title: str(doc.heading),
      address: str(doc.location),
      local_area: str(doc.local_area_name),
      price: num(doc.price_suggestion),
      price_total: num(doc.price_total),
      area: num(doc.area_range ?? doc.area ?? doc.primary_area),
      bedrooms: num(doc.number_of_bedrooms),
      property_type: str(doc.property_type_description),
      lat: float(coords.lat),
      lon: float(coords.lon),
      url: str(doc.canonical_url),
      images: images.map(normalizeImage),
    };
  }

  function findAds(data) {
    const ads = [];
    walk(data, (node) => {
      if (!Array.isArray(node) && node.ad_id != null && (node.heading || node.canonical_url)) {
        ads.push(node);
        return false;
      }
    });
    return ads;
  }

  function searchDataUrl(page) {
    const onSearch = location.pathname.startsWith("/realestate/homes/search.html");
    const params = new URLSearchParams(onSearch ? location.search : DEFAULT_SEARCH);
    params.delete("published");
    params.set("page", String(page));
    params.set("_routes", "routes/realestate/search/search");
    return `https://www.finn.no/realestate/homes/search.html.data?${params}`;
  }

  async function syncSearch() {
    const all = new Map();
    let complete = false;
    for (let page = 1; page <= MAX_SEARCH_PAGES && !stopRequested; page++) {
      status(`Search page ${page}… (${all.size} homes so far)`);
      const res = await fetch(searchDataUrl(page), { credentials: "include", headers: { Accept: "*/*" } });
      if (!res.ok) throw new Error(`finn returned ${res.status} for search page ${page}. Reload finn.no and try again.`);
      const data = decodeTurboStream(await res.text());
      const ads = findAds(data);
      let added = 0;
      for (const doc of ads) {
        const l = toListing(doc);
        if (l && !all.has(l.finnkode)) {
          all.set(l.finnkode, l);
          added++;
        }
      }
      if (page === 1) console.log("[boligjakt] first search page", { data, ads });
      const paging = findKey(data, ["paging"], (v) => v && typeof v === "object" && v.last != null);
      if (added === 0 || (paging && page >= num(paging.last))) {
        complete = true;
        break;
      }
      await politeDelay();
    }
    if (all.size === 0) throw new Error("Found no homes in the search results. finn may have changed its format; check the console.");
    await call("POST", "/api/ingest/search", { complete: complete && !stopRequested, listings: [...all.values()] });
    return all.size;
  }

  // ---------- sold ----------

  async function syncSold() {
    const max = Number(prompt("How many of the most recent sold homes in Stavanger do you want to sync?", "150"));
    if (!max) return 0;
    status("Fetching sold homes…");
    const res = await fetch(
      `https://www.finn.no/map/podium-resource/content/api/soldproperties?bbox=${encodeURIComponent(SOLD_BBOX)}`,
      { credentials: "include" },
    );
    if (!res.ok) throw new Error(`finn returned ${res.status} for sold homes`);
    const data = await res.json();
    const docs = (data.docs || []).filter((d) => d.adId).sort((a, b) => b.adId - a.adId).slice(0, max);
    await call("POST", "/api/ingest/sold", { docs });
    return docs.length;
  }

  // ---------- ad pages ----------

  function normalizeImage(url) {
    return url.replace(/\/dynamic\/[^/]+\//, "/dynamic/default/");
  }

  function extractImages(html, finnkode) {
    const clean = html.replace(/\\u002F/gi, "/").replace(/\\\//g, "/");
    const re = /https:\/\/images\.finncdn\.no\/dynamic\/[^/"'\s\\]+\/([^"'\s\\),]+)/g;
    // Image paths contain the finnkode either whole (item/479861168/...) or split in groups of three
    // (.../479/861/168_<uuid>.jpg).
    const id = String(finnkode);
    const grouped = id.replace(/(\d{3})(?=\d)/g, "$1/");
    const belongsToAd = (rest) => rest.includes(`/${id}/`) || rest.includes(`${grouped}_`) || rest.includes(`/${id}_`);
    const seen = new Set();
    const out = [];
    for (const m of clean.matchAll(re)) {
      const rest = m[1].replace(/&amp;/g, "&");
      if (!belongsToAd(rest) || seen.has(rest)) continue;
      seen.add(rest);
      out.push(`https://images.finncdn.no/dynamic/default/${rest}`);
    }
    return out;
  }

  function embeddedData(html) {
    const roots = [];
    const doc = new DOMParser().parseFromString(html, "text/html");
    for (const s of doc.querySelectorAll('script[type="application/ld+json"], script[type="application/json"]')) {
      try {
        roots.push(JSON.parse(s.textContent));
      } catch {
        // not JSON
      }
    }
    // React Router streams its loader data into the page as string chunks.
    let stream = "";
    for (const m of html.matchAll(/streamController\.enqueue\(("(?:[^"\\]|\\.)*")\)/g)) {
      try {
        stream += JSON.parse(m[1]);
      } catch {
        // ignore
      }
    }
    if (stream) {
      const decoded = decodeTurboStream(stream);
      if (decoded) roots.push(decoded);
    }
    const meta = (p) => doc.querySelector(`meta[property="${p}"]`)?.getAttribute("content") || null;
    return { roots, ogTitle: meta("og:title"), ogDescription: meta("og:description") };
  }

  // Returns the smallest object subtrees that mention this finnkode, so we read the ad's own data
  // rather than data about similar ads or the broker.
  function adSubtrees(roots, finnkode) {
    const hits = [];
    for (const root of roots) {
      walk(root, (node) => {
        for (const k of ["adId", "ad_id", "finnkode", "finnCode", "id"]) {
          if (num(node[k]) === finnkode && typeof node[k] !== "object") {
            hits.push(node);
            return;
          }
        }
      });
    }
    return [...hits, ...roots];
  }

  function parseAd(html, finnkode) {
    const { roots, ogTitle, ogDescription } = embeddedData(html);
    const trees = adSubtrees(roots, finnkode);
    const pick = (keys, accept) => {
      for (const t of trees) {
        const v = findKey(t, keys, accept);
        if (v != null) return v;
      }
      return null;
    };
    const isNum = (v) => num(v) != null && num(v) > 0;
    const isStr = (v) => typeof v === "string" && v.trim().length > 0;

    let address = pick(["address", "streetAddress", "location"], (v) => isStr(v) || (v && typeof v === "object" && (v.street || v.streetAddress)));
    if (address && typeof address === "object") {
      address = [address.street || address.streetAddress, [address.postalCode, address.postalName || address.addressLocality].filter(Boolean).join(" ")]
        .filter(Boolean)
        .join(", ");
    }

    let description = null;
    for (const t of trees) {
      walk(t, (node) => {
        for (const [k, v] of Object.entries(node)) {
          if (/description|generalText|adText/i.test(k) && typeof v === "string" && v.length > (description?.length || 0)) description = v;
        }
      });
      if (description) break;
    }

    const status = pick(["adStatus", "status", "state"], (v) => typeof v === "string" && /^(sold|solgt)$/i.test(v));

    return {
      finnkode,
      title: str(pick(["heading", "title"], isStr)) || ogTitle,
      address: str(address),
      price: num(pick(["askingPrice", "price_suggestion", "priceSuggestion", "price"], isNum)),
      price_total: num(pick(["totalPrice", "price_total", "priceTotal"], isNum)),
      area: num(pick(["usableArea", "primaryRoomArea", "internalArea", "area_range", "area", "size"], isNum)),
      bedrooms: num(pick(["noOfBedrooms", "numberOfBedrooms", "number_of_bedrooms", "bedrooms"], isNum)),
      property_type: str(pick(["propertyType", "property_type_description", "propertyTypeDescription"], isStr)),
      lat: float(pick(["lat", "latitude"], (v) => float(v) != null)),
      lon: float(pick(["lon", "lng", "longitude"], (v) => float(v) != null)),
      description: description ? stripHtml(description) : ogDescription,
      images: extractImages(html, finnkode),
      sold: Boolean(status),
    };
  }

  async function fetchAd(finnkode) {
    const res = await fetch(`https://www.finn.no/realestate/homes/ad.html?finnkode=${finnkode}`, { credentials: "include" });
    if (!res.ok) return null;
    const ad = parseAd(await res.text(), finnkode);
    ad.url = res.url;
    return ad;
  }

  async function fetchDetails() {
    const max = Number(prompt("How many homes should I fetch details for? (about 2-3 seconds each)", "100"));
    if (!max) return 0;
    const { finnkodes } = await call("GET", `/api/ingest/needed?limit=${max}`);
    let done = 0;
    for (const id of finnkodes) {
      if (stopRequested) break;
      status(`Fetching details ${done + 1}/${finnkodes.length} (finnkode ${id})…`);
      try {
        const ad = await fetchAd(id);
        if (ad && ad.images.length) {
          await call("POST", "/api/ingest/ad", ad);
          done++;
        } else {
          await call("POST", `/api/ingest/failed/${id}`);
        }
      } catch (e) {
        if (String(e.message).startsWith("could not reach")) throw e;
        console.warn("[boligjakt]", id, e);
        await call("POST", `/api/ingest/failed/${id}`).catch(() => {});
      }
      await politeDelay();
    }
    return done;
  }

  function currentFinnkode() {
    const m = location.search.match(/finnkode=(\d+)/) || location.pathname.match(/\/(\d{6,})(?:\/|$)/);
    return m ? Number(m[1]) : null;
  }

  async function ingestCurrentAd() {
    const id = currentFinnkode();
    if (!id) return null;
    const ad = parseAd(document.documentElement.outerHTML, id);
    ad.url = location.href;
    console.log("[boligjakt] parsed ad", ad);
    if (!ad.images.length) return null;
    await call("POST", "/api/ingest/ad", ad);
    return ad;
  }

  // ---------- panel ----------

  let statusEl;
  function status(text) {
    if (statusEl) statusEl.textContent = text;
  }

  async function run(label, fn, done) {
    stopRequested = false;
    try {
      status(`${label}…`);
      const result = await fn();
      status(done(result));
    } catch (e) {
      console.error("[boligjakt]", e);
      status(`Error: ${e.message}`);
    }
  }

  function panel() {
    const box = document.createElement("div");
    box.style.cssText =
      "position:fixed;right:16px;bottom:16px;z-index:2147483647;background:#fff;border:1px solid #cbd5e1;" +
      "border-radius:10px;box-shadow:0 4px 16px rgba(0,0,0,.15);padding:12px;font:14px system-ui,sans-serif;" +
      "color:#1b1b1f;width:240px;display:flex;flex-direction:column;gap:6px";
    const title = document.createElement("div");
    title.innerHTML = '<b style="color:#0063fb">boligjakt</b>';
    box.appendChild(title);

    const button = (text, onclick, primary) => {
      const b = document.createElement("button");
      b.textContent = text;
      b.style.cssText =
        `padding:6px 10px;border-radius:6px;cursor:pointer;font:inherit;text-align:left;` +
        (primary ? "background:#0063fb;color:#fff;border:1px solid #0063fb" : "background:#fff;border:1px solid #cbd5e1;color:#1b1b1f");
      b.onclick = onclick;
      box.appendChild(b);
      return b;
    };

    const onSearch = location.pathname.startsWith("/realestate/homes/search.html");
    button(onSearch ? "Sync this search" : "Sync Stavanger search", () =>
      run("Syncing search", syncSearch, (n) => `Synced ${n} homes. Now press Fetch details.`), true);
    button("Sync sold", () => run("Syncing sold", syncSold, (n) => `Synced ${n} sold homes. Now press Fetch details.`));
    button("Fetch details", () => run("Fetching details", fetchDetails, (n) => `Fetched details for ${n} homes.`));
    if (currentFinnkode()) {
      button("★ Favourite this home", () =>
        run("Saving favourite", async () => {
          await ingestCurrentAd();
          await call("POST", "/api/references", { input: String(currentFinnkode()) });
        }, () => "Added to favourites."));
    }
    button("Stop", () => {
      stopRequested = true;
      status("Stopping after the current request…");
    });

    statusEl = document.createElement("div");
    statusEl.style.cssText = "color:#64748b;font-size:12px;min-height:16px";
    box.appendChild(statusEl);

    const settings = document.createElement("a");
    settings.textContent = "Backend: " + backend();
    settings.href = "#";
    settings.style.cssText = "font-size:11px;color:#64748b";
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

  // Every ad you open on finn is saved, so favourites added by link get their photos.
  if (currentFinnkode()) {
    ingestCurrentAd()
      .then((ad) => ad && status(`Saved this home (${ad.images.length} photos).`))
      .catch((e) => status(`Error: ${e.message}`));
  }
})();
