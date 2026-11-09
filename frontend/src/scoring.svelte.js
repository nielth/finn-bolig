import { api } from "./api.js";

// Shared scoring status, polled while Claude is working so pages can refresh their lists.
export const scoring = $state({ running: false, pending: false, paused: false, scored: 0, total: 0, version: 0, error: "" });

export async function togglePause() {
  Object.assign(scoring, scoring.paused ? await api.resumeScoring() : await api.pauseScoring());
  pollScoring();
}

let timer;
export async function pollScoring() {
  clearTimeout(timer);
  try {
    Object.assign(scoring, { error: "" }, await api.scoring());
  } catch {
    // backend not reachable, try again later
  }
  timer = setTimeout(pollScoring, scoring.running || scoring.pending ? 3000 : 15000);
}
