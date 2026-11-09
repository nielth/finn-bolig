<script>
  import { onMount } from "svelte";
  import { api } from "../api.js";
  import ListingCard from "../components/ListingCard.svelte";
  import ListingDetail from "../components/ListingDetail.svelte";

  let refs = $state([]);
  let input = $state("");
  let error = $state("");
  let busy = $state(false);
  let selected = $state(null);

  async function load() {
    try {
      refs = await api.references();
    } catch (e) {
      error = e.message;
    }
  }

  async function add(e) {
    e.preventDefault();
    if (!input.trim()) return;
    busy = true;
    error = "";
    try {
      await api.addReference(input.trim());
      input = "";
      await load();
    } catch (err) {
      error = err.message;
    } finally {
      busy = false;
    }
  }

  async function remove(finnkode) {
    try {
      await api.removeReference(finnkode);
      await load();
    } catch (e) {
      error = e.message;
    }
  }

  const pending = $derived(refs.filter((r) => !r.images.length).length);

  onMount(() => {
    load();
    // Pick up details as soon as the userscript has fetched them.
    const timer = setInterval(() => pending && load(), 5000);
    return () => clearInterval(timer);
  });
</script>

<h1>My favourites</h1>
<p class="muted intro">
  Homes you like. Claude looks at their photos (outside and inside) to understand your taste, together with
  <a href="#/preferences">what you want</a>. Adding or removing a favourite rescores all homes.
</p>

<form onsubmit={add}>
  <input placeholder="Paste a finn.no link or finnkode" bind:value={input} />
  <button class="primary" disabled={busy}>Add</button>
</form>
{#if error}<p class="error">{error}</p>{/if}

{#if pending}
  <p class="notice">
    {pending} favourite{pending === 1 ? " is" : "s are"} waiting for photos. Open the ad on finn.no with the
    userscript installed, or press <b>Fetch details</b> in the userscript panel. See <a href="#/sync">Sync</a>.
  </p>
{/if}

{#if refs.length === 0}
  <p class="muted">No favourites yet. You can also press "★ Add to favourites" on any home, or in the userscript panel on finn.no.</p>
{:else}
  <div class="grid">
    {#each refs as ref (ref.finnkode)}
      <div class="ref">
        <ListingCard listing={ref} onopen={(l) => (selected = l.finnkode)} />
        <button class="danger remove" onclick={() => remove(ref.finnkode)}>Remove</button>
      </div>
    {/each}
  </div>
{/if}

{#if selected}
  <ListingDetail finnkode={selected} onclose={() => (selected = null)} onchange={load} />
{/if}

<style>
  h1 {
    margin: 0 0 8px;
  }

  .intro {
    max-width: 720px;
  }

  form {
    display: flex;
    gap: 8px;
    margin: 20px 0;
    max-width: 640px;
  }

  form input {
    flex: 1;
    min-width: 0;
  }

  .notice {
    background: var(--blue-light);
    border-radius: var(--radius);
    padding: 12px 16px;
  }

  .ref {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .remove {
    align-self: flex-start;
  }
</style>
