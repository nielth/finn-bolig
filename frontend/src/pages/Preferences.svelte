<script>
  import { onMount } from "svelte";
  import { api } from "../api.js";
  import { scoring, pollScoring, togglePause } from "../scoring.svelte.js";

  const examples = [
    {
      label: "Modern & funky",
      text: "I want a modern, funky house with bold architecture, large windows, open plan living and a lot of character. I care most about the exterior architecture and a bright, open living room and kitchen.",
    },
    {
      label: "Mid-century",
      text: "I want a mid-century style home (1950s-1970s) with wood panels, exposed brick, teak and warm natural materials, low roof lines and big windows. Original details are a plus. I care most about the exterior and the living room having a genuine mid-century feel.",
    },
    {
      label: "Classic villa",
      text: "I want a classic Norwegian villa (sveitserstil or funkis from the 1930s) with high ceilings, original details, a well-kept garden and light rooms.",
    },
  ];

  let prompt = $state("");
  let autoRescore = $state(true);
  let model = $state("");
  let models = $state([]);
  let saved = $state(null);
  let busy = $state(false);
  let error = $state("");
  let message = $state("");

  const dirty = $derived(
    saved && (prompt !== saved.prompt || autoRescore !== saved.auto_rescore || model !== currentModel(saved)),
  );
  const modelNote = $derived(models.find((m) => m.id === model)?.note || "");

  // An empty model in the settings means the default model.
  function currentModel(settings) {
    return settings.model || models.find((m) => m.default)?.id || "";
  }

  async function save() {
    busy = true;
    error = "";
    try {
      saved = await api.saveSettings({ prompt, auto_rescore: autoRescore, model });
      message = autoRescore
        ? "Saved. Claude will rescore all homes in a few seconds, and the list updates as scores come in."
        : "Saved. Automatic rescoring is off, so press Rescore now to update the scores.";
      pollScoring();
    } catch (e) {
      error = e.message;
    } finally {
      busy = false;
    }
  }

  async function rescore() {
    await api.runScoring();
    message = "Scoring scheduled.";
    pollScoring();
  }

  onMount(async () => {
    try {
      [saved, models] = await Promise.all([api.settings(), api.models()]);
      model = currentModel(saved);
      prompt = saved.prompt;
      autoRescore = saved.auto_rescore;
    } catch (e) {
      error = e.message;
    }
  });
</script>

<h1>What I want</h1>
<p class="muted intro">
  Describe the style you are after and what you care about most. Claude only looks at the photos of each home (outside
  and inside) and compares them with this description and <a href="#/references">your favourites</a>. The ad text is
  not used.
</p>

<div class="examples">
  <span class="muted">Start from:</span>
  {#each examples as ex}
    <button class="chip" onclick={() => (prompt = ex.text)}>{ex.label}</button>
  {/each}
</div>

<textarea rows="10" bind:value={prompt}></textarea>

<label class="model">
  <span>Model</span>
  <select bind:value={model}>
    {#each models as m}
      <option value={m.id}>{m.label}</option>
    {/each}
  </select>
  <span class="muted">{modelNote}</span>
</label>

<label class="auto">
  <input type="checkbox" bind:checked={autoRescore} />
  Automatically rescore all homes when this or my favourites change
</label>

<div class="actions">
  <button class="primary" onclick={save} disabled={busy || !dirty}>Save and update list</button>
  <button onclick={rescore} disabled={scoring.paused}>Rescore now</button>
  <button onclick={togglePause}>{scoring.paused ? "▶ Resume scoring" : "❚❚ Pause scoring"}</button>
</div>

{#if error}<p class="error">{error}</p>{/if}
{#if message}<p class="notice">{message}</p>{/if}

<div class="status">
  {#if scoring.paused}
    Scoring is paused. {scoring.scored} of {scoring.total} homes are scored for the current description.
  {:else if scoring.running}
    <div class="progress"><div style="width: {scoring.total ? (100 * scoring.scored) / scoring.total : 0}%"></div></div>
    Claude has scored {scoring.scored} of {scoring.total} homes.
  {:else if scoring.pending}
    Scoring starts in a few seconds…
  {:else}
    {scoring.scored} of {scoring.total} homes are scored for the current description.
  {/if}
  {#if scoring.error}<p class="error">Last error: {scoring.error}</p>{/if}
</div>

<style>
  h1 {
    margin: 0 0 8px;
  }

  .intro {
    max-width: 720px;
  }

  .examples {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    margin: 20px 0 12px;
  }

  textarea {
    width: 100%;
    max-width: 840px;
    display: block;
    line-height: 1.5;
    resize: vertical;
  }

  .model {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 12px;
    align-items: center;
    margin-top: 14px;
  }

  .auto {
    display: flex;
    gap: 8px;
    align-items: center;
    margin: 14px 0;
  }

  .actions {
    display: flex;
    gap: 8px;
  }

  .notice {
    background: var(--blue-light);
    border-radius: var(--radius);
    padding: 12px 16px;
    max-width: 840px;
  }

  .status {
    margin-top: 24px;
    color: var(--muted);
    max-width: 840px;
  }

  .progress {
    height: 8px;
    background: var(--bg-soft);
    border-radius: 999px;
    overflow: hidden;
    margin-bottom: 8px;
  }

  .progress div {
    height: 100%;
    background: var(--blue);
    transition: width 0.3s;
  }
</style>
