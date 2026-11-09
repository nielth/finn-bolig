<script>
  import { onMount } from "svelte";
  import { api } from "../api.js";

  let prompt = $state("");
  let saved = $state("");
  let busy = $state(false);
  let error = $state("");
  let message = $state("");

  async function save() {
    busy = true;
    error = "";
    try {
      saved = (await api.saveSettings({ prompt })).prompt;
      message = "Saved.";
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }

  onMount(async () => {
    try {
      saved = (await api.settings()).prompt;
      prompt = saved;
    } catch (e) {
      error = e.message;
    }
  });
</script>

<h1>What I want</h1>
<p class="muted intro">
  Describe the style you are after and what you care about most. Claude will use this, together with
  <a href="#/references">your favourites</a>, to rank the homes.
</p>
<p class="notice">
  AI ranking is being rebuilt: Claude will first write a short summary of every home from all its photos and the ad
  text, and then score those summaries against this description. Until then, your description is saved here.
</p>

<textarea rows="12" bind:value={prompt}></textarea>

<div class="actions">
  <button class="primary" onclick={save} disabled={busy || prompt === saved}>Save</button>
</div>

{#if error}<p class="error">{error}</p>{/if}
{#if message}<p class="muted">{message}</p>{/if}

<style>
  h1 {
    margin: 0 0 8px;
  }

  .intro,
  .notice,
  textarea {
    max-width: 840px;
  }

  .notice {
    background: var(--blue-light);
    border-radius: var(--radius);
    padding: 12px 16px;
  }

  textarea {
    width: 100%;
    display: block;
    line-height: 1.5;
    resize: vertical;
    margin-top: 16px;
  }

  .actions {
    margin-top: 12px;
  }
</style>
