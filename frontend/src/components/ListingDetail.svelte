<script>
  import { onMount } from "svelte";
  import { api } from "../api.js";
  import { price, finnUrl, scoreClass } from "../format.js";

  let { finnkode, onclose, onchange = () => {} } = $props();

  let listing = $state(null);
  let error = $state("");
  let index = $state(0);
  let busy = $state(false);
  let showFullDescription = $state(false);
  let thumbs = $state();

  async function load() {
    try {
      listing = await api.listing(finnkode);
    } catch (e) {
      error = e.message;
    }
  }

  const images = $derived(listing?.images || []);

  function go(i) {
    if (!images.length) return;
    index = (i + images.length) % images.length;
    thumbs?.children[index]?.scrollIntoView({ block: "nearest", inline: "center", behavior: "smooth" });
  }

  function onkey(e) {
    if (e.key === "Escape") onclose();
    if (e.key === "ArrowRight") go(index + 1);
    if (e.key === "ArrowLeft") go(index - 1);
  }

  async function toggleReference() {
    busy = true;
    try {
      if (listing.is_reference) await api.removeReference(listing.finnkode);
      else await api.addReference(String(listing.finnkode));
      await load();
      onchange();
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }

  onMount(() => {
    load();
    document.body.style.overflow = "hidden";
    return () => (document.body.style.overflow = "");
  });
</script>

<svelte:window onkeydown={onkey} />

<div class="overlay" role="dialog" aria-modal="true">
  <div class="top">
    <button onclick={onclose}>← Back</button>
    {#if listing}
      <a class="finn" href={finnUrl(listing)} target="_blank" rel="noreferrer">Open on finn.no ↗</a>
    {/if}
  </div>

  {#if error}
    <p class="error container">{error}</p>
  {:else if listing}
    <div class="content">
      <section class="gallery">
        <div class="stage">
          {#if images.length}
            <img src={images[index]} alt="Photo {index + 1}" referrerpolicy="no-referrer" />
            <button class="arrow prev" onclick={() => go(index - 1)} aria-label="Previous photo">‹</button>
            <button class="arrow next" onclick={() => go(index + 1)} aria-label="Next photo">›</button>
            <span class="count">{index + 1} / {images.length}</span>
          {:else}
            <div class="empty">No images yet. Open this ad on finn.no with the userscript, or run "Fetch details" from the sync panel.</div>
          {/if}
        </div>
        {#if images.length > 1}
          <div class="thumbs" bind:this={thumbs}>
            {#each images as src, i}
              <button class="thumb" class:active={i === index} onclick={() => go(i)} aria-label="Photo {i + 1}">
                <img {src} alt="" loading="lazy" referrerpolicy="no-referrer" />
              </button>
            {/each}
          </div>
        {/if}
      </section>

      <section class="info">
        <div class="muted">{listing.local_area || listing.district || ""}</div>
        <h1>{listing.title || `Finnkode ${listing.finnkode}`}</h1>
        <div class="address">{listing.address || ""}</div>

        {#if listing.status === "sold"}
          <div class="sold">Sold</div>
        {/if}

        <div class="claude">
          <span class="score {scoreClass(listing.score)}">{listing.score ?? "–"}</span>
          <div>
            {#if listing.style}<div class="style">{listing.style}</div>{/if}
            <div class="muted">
              {listing.summary || listing.score_error || "Claude has not scored this home yet."}
            </div>
          </div>
        </div>

        <dl>
          {#if listing.price}<dt>Asking price</dt><dd class="big">{price(listing.price)}</dd>{/if}
          {#if listing.price_total}<dt>Total price</dt><dd>{price(listing.price_total)}</dd>{/if}
          {#if listing.area}<dt>Size</dt><dd>{listing.area} m²</dd>{/if}
          {#if listing.bedrooms}<dt>Bedrooms</dt><dd>{listing.bedrooms}</dd>{/if}
          {#if listing.property_type}<dt>Type</dt><dd>{listing.property_type}</dd>{/if}
          {#if listing.district}<dt>District</dt><dd>{listing.district}</dd>{/if}
          <dt>Finnkode</dt><dd>{listing.finnkode}</dd>
        </dl>

        <button class={listing.is_reference ? "danger" : "primary"} onclick={toggleReference} disabled={busy}>
          {listing.is_reference ? "Remove from favourites" : "★ Add to favourites"}
        </button>

        {#if listing.description}
          <div class="description" class:clamped={!showFullDescription}>{listing.description}</div>
          <button class="link" onclick={() => (showFullDescription = !showFullDescription)}>
            {showFullDescription ? "Show less" : "Show more"}
          </button>
        {/if}
      </section>
    </div>
  {:else}
    <p class="container muted">Loading…</p>
  {/if}
</div>

<style>
  .overlay {
    position: fixed;
    inset: 0;
    background: var(--bg);
    z-index: 100;
    overflow-y: auto;
  }

  .top {
    position: sticky;
    top: 0;
    z-index: 2;
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
    background: var(--bg);
    border-bottom: 1px solid var(--border);
  }

  .content {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 380px;
    gap: 32px;
    max-width: 1400px;
    margin: 0 auto;
    padding: 24px 16px 64px;
  }

  .stage {
    position: relative;
    background: #111;
    border-radius: var(--radius);
    overflow: hidden;
    aspect-ratio: 3 / 2;
    max-height: 75vh;
    width: 100%;
  }

  .stage img {
    width: 100%;
    height: 100%;
    object-fit: contain;
    display: block;
  }

  .empty {
    display: grid;
    place-items: center;
    height: 100%;
    color: #cbd5e1;
    padding: 24px;
    text-align: center;
  }

  .arrow {
    position: absolute;
    top: 50%;
    transform: translateY(-50%);
    width: 48px;
    height: 48px;
    border-radius: 50%;
    border: none;
    background: rgba(255, 255, 255, 0.9);
    font-size: 30px;
    line-height: 1;
    padding: 0;
  }

  .prev {
    left: 12px;
  }

  .next {
    right: 12px;
  }

  .count {
    position: absolute;
    bottom: 12px;
    left: 50%;
    transform: translateX(-50%);
    background: rgba(0, 0, 0, 0.6);
    color: #fff;
    padding: 4px 12px;
    border-radius: 999px;
    font-size: 14px;
  }

  .thumbs {
    display: flex;
    gap: 8px;
    overflow-x: auto;
    padding: 12px 0;
  }

  .thumb {
    flex: 0 0 auto;
    padding: 0;
    width: 96px;
    height: 72px;
    overflow: hidden;
    border: 2px solid transparent;
    opacity: 0.7;
  }

  .thumb.active {
    border-color: var(--blue);
    opacity: 1;
  }

  .thumb img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  h1 {
    font-size: 24px;
    margin: 4px 0;
    line-height: 1.25;
  }

  .address {
    color: var(--muted);
  }

  .sold {
    display: inline-block;
    margin-top: 10px;
    background: var(--red);
    color: #fff;
    font-weight: 700;
    padding: 4px 10px;
    border-radius: 4px;
  }

  .claude {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    margin: 20px 0;
    padding: 14px;
    border-radius: var(--radius);
    background: var(--bg-soft);
  }

  .style {
    font-weight: 600;
    margin-bottom: 2px;
  }

  dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 8px 16px;
    margin: 0 0 20px;
  }

  dt {
    color: var(--muted);
  }

  dd {
    margin: 0;
    font-weight: 600;
  }

  dd.big {
    font-size: 20px;
  }

  .description {
    margin-top: 24px;
    white-space: pre-line;
    line-height: 1.5;
  }

  .description.clamped {
    max-height: 9em;
    overflow: hidden;
  }

  .link {
    border: none;
    padding: 4px 0;
    color: var(--blue);
    background: none;
  }

  @media (max-width: 900px) {
    .content {
      grid-template-columns: 1fr;
      padding-top: 0;
      padding-left: 0;
      padding-right: 0;
    }
    .stage {
      border-radius: 0;
    }
    .thumbs,
    .info {
      padding-left: 16px;
      padding-right: 16px;
    }
  }
</style>
