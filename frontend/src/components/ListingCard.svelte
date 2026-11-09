<script>
  import { price, finnUrl, scoreClass, daysAgo, eventLabel } from "../format.js";

  let { listing, onopen } = $props();

  let index = $state(0);
  const images = $derived(listing.images || []);
  // Show recent changes, like a new listing or a price drop, as a badge.
  const recent = $derived(listing.last_event && daysAgo(listing.last_event.at) < 7 ? listing.last_event : null);

  function step(e, delta) {
    e.stopPropagation();
    index = (index + delta + images.length) % images.length;
  }
</script>

<article>
  <button class="open" onclick={() => onopen(listing)} aria-label="Open {listing.title || listing.finnkode}">
    <div class="image">
      {#if images.length}
        <img src={images[index]} alt="" loading="lazy" referrerpolicy="no-referrer" />
        {#if images.length > 1}
          <span class="nav prev" role="presentation" onclick={(e) => step(e, -1)}>‹</span>
          <span class="nav next" role="presentation" onclick={(e) => step(e, 1)}>›</span>
          <span class="count">{index + 1}/{images.length}</span>
        {/if}
      {:else}
        <div class="placeholder">No images yet</div>
      {/if}
      {#if listing.status === "sold"}
        <span class="sold">SOLD</span>
      {:else if recent}
        <span class="event">{eventLabel(recent)}</span>
      {/if}
      {#if listing.score != null}
        <span class="score {scoreClass(listing.score)} badge" title={listing.summary || ""}>{listing.score}</span>
      {/if}
    </div>
    <div class="body">
      <div class="area muted">{listing.local_area || listing.district || ""}</div>
      <h3>{listing.title || `Finnkode ${listing.finnkode}`}</h3>
      <div class="address muted">{listing.address || ""}</div>
      <div class="facts">
        {#if listing.area}<span>{listing.area} m²</span>{/if}
        {#if listing.price}<span class="price">{price(listing.price)}</span>{/if}
      </div>
      <div class="sub muted">
        {#if listing.price_total}Total {price(listing.price_total)}{/if}
        {#if listing.bedrooms}· {listing.bedrooms} bedrooms{/if}
        {#if listing.property_type}· {listing.property_type}{/if}
      </div>
      {#if listing.style}
        <div class="style">{listing.style}</div>
      {/if}
    </div>
  </button>
  <a class="finn" href={finnUrl(listing)} target="_blank" rel="noreferrer" title="Open the ad on finn.no">finn.no ↗</a>
</article>

<style>
  article {
    position: relative;
  }

  .finn {
    position: absolute;
    top: 8px;
    left: 8px;
    background: rgba(255, 255, 255, 0.95);
    color: var(--blue);
    font-size: 13px;
    font-weight: 600;
    padding: 4px 10px;
    border-radius: 999px;
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.25);
  }

  .finn:hover {
    background: var(--blue);
    color: #fff;
    text-decoration: none;
  }

  .open {
    all: unset;
    display: block;
    width: 100%;
    cursor: pointer;
  }

  .open:focus-visible {
    outline: 2px solid var(--blue);
    outline-offset: 4px;
    border-radius: var(--radius);
  }

  .image {
    position: relative;
    aspect-ratio: 4 / 3;
    border-radius: var(--radius);
    overflow: hidden;
    background: var(--bg-soft);
  }

  img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
  }

  .placeholder {
    display: grid;
    place-items: center;
    height: 100%;
    color: var(--muted);
    font-size: 14px;
  }

  .nav {
    position: absolute;
    top: 50%;
    transform: translateY(-50%);
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: rgba(255, 255, 255, 0.9);
    display: grid;
    place-items: center;
    font-size: 24px;
    line-height: 1;
    opacity: 0;
    transition: opacity 0.15s;
  }

  .image:hover .nav {
    opacity: 1;
  }

  .prev {
    left: 8px;
  }

  .next {
    right: 8px;
  }

  .count {
    position: absolute;
    bottom: 8px;
    right: 8px;
    background: rgba(0, 0, 0, 0.6);
    color: #fff;
    font-size: 12px;
    padding: 2px 8px;
    border-radius: 999px;
  }

  .sold {
    position: absolute;
    bottom: 8px;
    left: 8px;
    background: var(--red);
    color: #fff;
    font-weight: 700;
    font-size: 12px;
    padding: 4px 8px;
    border-radius: 4px;
  }

  .event {
    position: absolute;
    bottom: 8px;
    left: 8px;
    background: var(--blue);
    color: #fff;
    font-weight: 600;
    font-size: 12px;
    padding: 4px 8px;
    border-radius: 4px;
  }

  .badge {
    position: absolute;
    top: 8px;
    right: 8px;
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.25);
  }

  .body {
    padding-top: 10px;
  }

  .area {
    font-size: 13px;
  }

  h3 {
    margin: 2px 0;
    font-size: 17px;
    line-height: 1.3;
    font-weight: 600;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }

  .open:hover h3 {
    color: var(--blue);
  }

  .address {
    font-size: 14px;
  }

  .facts {
    display: flex;
    justify-content: space-between;
    font-weight: 700;
    margin-top: 6px;
  }

  .sub {
    font-size: 13px;
    margin-top: 2px;
  }

  .style {
    margin-top: 6px;
    font-size: 13px;
    color: var(--blue-dark);
  }
</style>
