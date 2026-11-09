<script>
  import { onMount } from "svelte";
  import { api } from "../api.js";
  import { scoring } from "../scoring.svelte.js";
  import ListingCard from "../components/ListingCard.svelte";
  import ListingDetail from "../components/ListingDetail.svelte";

  const STORAGE_KEY = "boligjakt-filters";

  function loadFilters() {
    const defaults = { status: "active", districts: [], priceMin: "", priceMax: "", sort: "score" };
    try {
      return { ...defaults, ...JSON.parse(localStorage.getItem(STORAGE_KEY) || "{}") };
    } catch {
      return defaults;
    }
  }

  let filters = $state(loadFilters());
  let listings = $state([]);
  let districts = $state([]);
  let loading = $state(true);
  let error = $state("");
  let selected = $state(null);

  async function load() {
    try {
      const params = { status: filters.status, sort: filters.sort };
      if (filters.districts.length) params.districts = filters.districts.join(",");
      if (filters.priceMin) params.price_min = filters.priceMin;
      if (filters.priceMax) params.price_max = filters.priceMax;
      listings = await api.listings(params);
      error = "";
    } catch (e) {
      error = e.message;
    } finally {
      loading = false;
    }
  }

  // Reload whenever a filter changes.
  $effect(() => {
    const snapshot = JSON.stringify(filters);
    try {
      localStorage.setItem(STORAGE_KEY, snapshot);
    } catch {
      // storage unavailable, filters just won't be remembered
    }
    load();
  });

  // Reload when Claude has scored more homes, so the order updates as scores arrive.
  let lastScored = 0;
  $effect(() => {
    const key = scoring.scored * 1000003 + scoring.version;
    if (key !== lastScored) {
      lastScored = key;
      load();
    }
  });

  function toggleDistrict(name) {
    filters.districts = filters.districts.includes(name)
      ? filters.districts.filter((d) => d !== name)
      : [...filters.districts, name];
  }

  onMount(async () => {
    try {
      districts = await api.districts();
    } catch {
      // filter just won't show counts
    }
  });
</script>

<section class="filters">
  <div class="row">
    <div class="segmented" role="group" aria-label="Status">
      {#each [["active", "For sale"], ["sold", "Sold"], ["all", "Both"]] as [value, label]}
        <button class:active={filters.status === value} onclick={() => (filters.status = value)}>{label}</button>
      {/each}
    </div>

    <label class="price">
      <span class="muted">Price</span>
      <input type="number" placeholder="Min" step="100000" min="0" bind:value={filters.priceMin} />
      <span>–</span>
      <input type="number" placeholder="Max" step="100000" min="0" bind:value={filters.priceMax} />
    </label>

    <label>
      <span class="muted">Sort</span>
      <select bind:value={filters.sort}>
        <option value="score">Best match</option>
        <option value="newest">Newest</option>
        <option value="price_asc">Lowest price</option>
        <option value="price_desc">Highest price</option>
      </select>
    </label>
  </div>

  <div class="row districts">
    <button class="chip" class:active={filters.districts.length === 0} onclick={() => (filters.districts = [])}>
      All of Stavanger
    </button>
    {#each districts as d}
      <button class="chip" class:active={filters.districts.includes(d.name)} onclick={() => toggleDistrict(d.name)}>
        {d.name} <span class="muted">{d.count}</span>
      </button>
    {/each}
  </div>
</section>

<div class="summary">
  <h2>{listings.length} {listings.length === 1 ? "home" : "homes"}</h2>
  {#if filters.sort === "score"}
    <span class="muted">sorted by how well Claude thinks they match what you want</span>
  {/if}
</div>

{#if error}
  <p class="error">{error}</p>
{:else if loading}
  <p class="muted">Loading…</p>
{:else if listings.length === 0}
  <div class="empty">
    <p>No homes here yet.</p>
    <p class="muted">Use the userscript on finn.no to sync a search. See <a href="#/sync">Sync</a>.</p>
  </div>
{:else}
  <div class="grid">
    {#each listings as listing (listing.finnkode)}
      <ListingCard {listing} onopen={(l) => (selected = l.finnkode)} />
    {/each}
  </div>
{/if}

{#if selected}
  <ListingDetail finnkode={selected} onclose={() => (selected = null)} onchange={load} />
{/if}

<style>
  .filters {
    display: flex;
    flex-direction: column;
    gap: 14px;
    padding-bottom: 20px;
    border-bottom: 1px solid var(--border);
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    gap: 12px 24px;
    align-items: center;
  }

  .districts {
    gap: 8px;
  }

  label {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .price input {
    width: 130px;
  }

  .segmented {
    display: inline-flex;
    border: 1px solid #cbd5e1;
    border-radius: var(--radius);
    overflow: hidden;
  }

  .segmented button {
    border: none;
    border-radius: 0;
    padding: 8px 16px;
  }

  .segmented button + button {
    border-left: 1px solid #cbd5e1;
  }

  .segmented button.active {
    background: var(--blue);
    color: #fff;
    font-weight: 600;
  }

  .summary {
    display: flex;
    align-items: baseline;
    gap: 12px;
    flex-wrap: wrap;
    margin: 20px 0;
  }

  h2 {
    margin: 0;
    font-size: 20px;
  }

  .empty {
    padding: 48px 0;
    text-align: center;
  }

  @media (max-width: 600px) {
    .price input {
      width: 100px;
    }
  }
</style>
