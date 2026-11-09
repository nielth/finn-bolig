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
