<script>
  import { onMount } from "svelte";
  import Listings from "./pages/Listings.svelte";
  import References from "./pages/References.svelte";
  import Preferences from "./pages/Preferences.svelte";
  import Sync from "./pages/Sync.svelte";
  import { crawl, pollCrawl } from "./crawl.svelte.js";
  import { dateTime } from "./format.js";

  const routes = { "/": Listings, "/references": References, "/preferences": Preferences, "/sync": Sync };

  let path = $state(location.hash.slice(1) || "/");
  const Page = $derived(routes[path] || Listings);

  onMount(() => {
    const onHash = () => (path = location.hash.slice(1) || "/");
    window.addEventListener("hashchange", onHash);
    pollCrawl();
    return () => window.removeEventListener("hashchange", onHash);
  });
</script>

<header>
  <div class="container bar">
    <a class="logo" href="#/">boligjakt</a>
    <nav>
      <a href="#/" class:active={path === "/"}>Homes</a>
      <a href="#/references" class:active={path === "/references"}>My favourites</a>
      <a href="#/preferences" class:active={path === "/preferences"}>What I want</a>
      <a href="#/sync" class:active={path === "/sync"}>Sync</a>
    </nav>
    <div class="status">
      {#if crawl.running}
        <span class="dot"></span> Crawling finn: {crawl.running.pages} search pages, {crawl.running.pages_fetched} ads
      {:else if crawl.last}
        Updated {dateTime(crawl.last.finished_at)}
      {:else}
        Not crawled yet
      {/if}
    </div>
  </div>
</header>

<main class="container">
  <Page />
</main>

<style>
  header {
    border-bottom: 1px solid var(--border);
    background: var(--bg);
    position: sticky;
    top: 0;
    z-index: 10;
  }

  .bar {
    display: flex;
    align-items: center;
    gap: 28px;
    height: 64px;
  }

  .logo {
    font-weight: 800;
    font-size: 22px;
    color: var(--blue);
    letter-spacing: -0.5px;
  }

  .logo:hover {
    text-decoration: none;
  }

  nav {
    display: flex;
    gap: 20px;
    overflow-x: auto;
  }

  nav a {
    color: var(--text);
    font-weight: 500;
    padding: 20px 0;
    border-bottom: 3px solid transparent;
    white-space: nowrap;
  }

  nav a:hover {
    text-decoration: none;
    color: var(--blue);
  }

  nav a.active {
    border-bottom-color: var(--blue);
    color: var(--blue);
  }

  .status {
    margin-left: auto;
    font-size: 14px;
    color: var(--muted);
    white-space: nowrap;
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--blue);
    animation: pulse 1.2s infinite;
  }

  @keyframes pulse {
    50% {
      opacity: 0.3;
    }
  }

  main {
    padding-top: 24px;
    padding-bottom: 64px;
  }

  @media (max-width: 720px) {
    .bar {
      gap: 16px;
    }
    .status {
      display: none;
    }
  }
</style>
