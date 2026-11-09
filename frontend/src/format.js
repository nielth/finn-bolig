const nok = new Intl.NumberFormat("nb-NO");

export function price(n) {
  return n == null ? "" : `${nok.format(n)} kr`;
}

export function finnUrl(l) {
  return l.url || `https://www.finn.no/realestate/homes/ad.html?finnkode=${l.finnkode}`;
}

export function scoreClass(score) {
  if (score == null) return "none";
  if (score >= 75) return "high";
  if (score >= 50) return "mid";
  return "low";
}

export function date(iso) {
  return iso ? new Date(iso).toLocaleDateString("nb-NO", { day: "numeric", month: "short", year: "numeric" }) : "";
}

export function dateTime(iso) {
  return iso ? new Date(iso).toLocaleString("nb-NO", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" }) : "";
}

export function daysAgo(iso) {
  return iso ? (Date.now() - new Date(iso).getTime()) / 86400000 : Infinity;
}

const eventLabels = {
  new: "New",
  price_change: "Price changed",
  updated: "Ad updated",
  sold: "Sold",
  removed: "Removed from finn",
  relisted: "Back on finn",
};

export function eventLabel(e) {
  return eventLabels[e.kind] || e.kind;
}
