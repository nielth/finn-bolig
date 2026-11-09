<script>
  import { onMount } from "svelte";
  import { api } from "../api.js";
  import { crawl } from "../crawl.svelte.js";
  import { dateTime } from "../format.js";

  const scriptUrl = `${location.origin}/finn-bolig.user.js`;
  let crawls = $state([]);

  async function load() {
    try {
      crawls = await api.crawls();
    } catch {
      // shown as empty
    }
  }

  onMount(() => {
    load();
    const timer = setInterval(load, 10000);
    return () => clearInterval(timer);
  });
</script>

<h1>Crawling finn.no</h1>
<div class="text">
  <p>
    finn.no blocks automated crawlers, so the crawler runs in your own browser as a userscript. Once a day, while you
    have a finn.no tab open, it goes through every home for sale in Stavanger, fetches the ad pages for new, changed and
    disappeared homes, and sends everything to this app. It waits 2–4 seconds between requests. The first crawl takes
    about 20–30 minutes; after that a daily crawl takes a few minutes.
  </p>

  <h2>Setup (once)</h2>
  <ol>
    <li>Install <a href="https://violentmonkey.github.io/" target="_blank" rel="noreferrer">Violentmonkey</a> or Tampermonkey.</li>
    <li>Open <a href={scriptUrl}>{scriptUrl}</a> and press Install (or Update).</li>
    <li>Open <a href="https://www.finn.no" target="_blank" rel="noreferrer">finn.no</a>. The crawl starts by itself when it is due. You can also press <b>Crawl now</b> in the small panel at the bottom right.</li>
  </ol>
  <p class="muted">
    Keep the finn tab visible during the first crawl. Browsers slow down timers in background tabs, which makes the
    crawl much slower. If finn asks you to prove you are human, complete the check and press Crawl now again.
  </p>
</div>

<h2>Crawl history</h2>
{#if crawl.running}
  <p class="notice">
    Crawling now: {crawl.running.pages} search pages, {crawl.running.pages_fetched} ad pages so far.
  </p>
{/if}
{#if crawls.length === 0}
  <p class="muted">No crawls yet.</p>
{:else}
  <div class="table">
    <table>
      <thead>
        <tr>
          <th>Started</th>
          <th>Homes found</th>
          <th>Ads fetched</th>
          <th>New</th>
          <th>Updated</th>
          <th>Sold</th>
          <th>Removed</th>
          <th>Result</th>
        </tr>
      </thead>
      <tbody>
        {#each crawls as c (c.id)}
          <tr>
            <td>{dateTime(c.started_at)}</td>
            <td>{c.found || "–"}</td>
            <td>{c.pages_fetched}</td>
            <td>{c.new_ads}</td>
            <td>{c.updated_ads}</td>
            <td>{c.sold_ads}</td>
            <td>{c.removed_ads}</td>
            <td class:error={c.error && c.error !== "stopped"}>
              {#if !c.finished_at}
                running…
              {:else if c.error}
                {c.error}
              {:else if !c.search_complete}
                incomplete search
              {:else}
                ✓
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  h1 {
    margin: 0 0 8px;
  }

  h2 {
    font-size: 18px;
    margin-top: 28px;
  }

  .text {
    max-width: 720px;
    line-height: 1.55;
  }

  .notice {
    background: var(--blue-light);
    border-radius: var(--radius);
    padding: 12px 16px;
  }

  .table {
    overflow-x: auto;
  }

  table {
    border-collapse: collapse;
    font-size: 14px;
    min-width: 640px;
  }

  th,
  td {
    text-align: left;
    padding: 8px 14px 8px 0;
    border-bottom: 1px solid var(--border);
    white-space: nowrap;
  }

  th {
    color: var(--muted);
    font-weight: 500;
  }
</style>
