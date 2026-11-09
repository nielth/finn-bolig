<script>
  const scriptUrl = `${location.origin}/finn-bolig.user.js`;
</script>

<h1>Sync from finn.no</h1>
<div class="text">
  <p>
    finn.no blocks automated requests from servers, so this app never contacts finn itself. A small userscript runs in
    your own browser while you are on finn.no and sends the homes to this app. It goes slowly (about one page every
    2 seconds) to keep the load on finn low.
  </p>

  <h2>1. Install the userscript</h2>
  <ol>
    <li>Install <a href="https://violentmonkey.github.io/" target="_blank" rel="noreferrer">Violentmonkey</a> or Tampermonkey in your browser.</li>
    <li>Open <a href={scriptUrl}>{scriptUrl}</a> and press Install.</li>
  </ol>

  <h2>2. Sync homes for sale</h2>
  <ol>
    <li>On finn.no, search for homes in Stavanger with the filters you want (for example max price).</li>
    <li>Press <b>Sync this search</b> in the small "boligjakt" panel at the bottom right.</li>
    <li>Press <b>Fetch details</b> to get all photos for each home. Claude needs these to score the homes.</li>
  </ol>

  <h2>3. Sync sold homes</h2>
  <p>Press <b>Sync sold</b> in the panel (on any finn.no page). It fetches sold homes in Stavanger from finn's sold-homes map, and then <b>Fetch details</b> gets their photos.</p>

  <h2>Favourites</h2>
  <p>
    On any finn ad, press <b>★ Favourite</b> in the panel. Every ad you open on finn.no while the userscript is running
    is also saved, so favourites added here by link get their photos the next time you open them or run Fetch details.
  </p>
</div>

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
</style>
