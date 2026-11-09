import { api } from "./api.js";

// Shared crawl status, shown in the header and on the Sync page.
export const crawl = $state({ running: null, last: null, due: false });

let timer;
export async function pollCrawl() {
  clearTimeout(timer);
  try {
    Object.assign(crawl, await api.crawlStatus());
  } catch {
    // backend not reachable, try again later
  }
  timer = setTimeout(pollCrawl, crawl.running ? 5000 : 60000);
}
