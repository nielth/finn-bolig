async function request(method, path, body) {
  const res = await fetch(`/api${path}`, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204) return null;
  const data = await res.json().catch(() => null);
  if (!res.ok) throw new Error(data?.error || `${res.status} ${res.statusText}`);
  return data;
}

export const api = {
  listings: (params) => request("GET", `/listings?${new URLSearchParams(params)}`),
  listing: (id) => request("GET", `/listings/${id}`),
  districts: () => request("GET", "/districts"),
  references: () => request("GET", "/references"),
  addReference: (input) => request("POST", "/references", { input }),
  removeReference: (id) => request("DELETE", `/references/${id}`),
  settings: () => request("GET", "/settings"),
  saveSettings: (settings) => request("PUT", "/settings", settings),
  crawlStatus: () => request("GET", "/crawl/status"),
  crawls: () => request("GET", "/crawls"),
};
