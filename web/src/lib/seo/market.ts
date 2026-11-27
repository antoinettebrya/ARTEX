import type { Market } from "./research";

export function readMarketFilter(params: Pick<URLSearchParams, "get">): Market | undefined {
  const country = params.get("country_code")?.toUpperCase();
  const location = Number(params.get("location_code"));
  const language = params.get("language_code");
  const engine = params.get("search_engine");
  const device = params.get("device");
  if (
    country &&
    /^[A-Z]{2}$/.test(country) &&
    Number.isInteger(location) &&
    location >= 1 &&
    location <= 100_000_000 &&
    language &&
    /^[A-Za-z_-]{2,20}$/.test(language) &&
    (engine === "google" || engine === "bing") &&
    (device === "desktop" || device === "mobile")
  ) {
    return {
      country_code: country,
      location_code: location,
      language_code: language,
      search_engine: engine,
      device,
    };
  }
}

export function marketFilterChanges(market?: Market) {
  return {
    country_code: market?.country_code ?? null,
    location_code: market ? String(market.location_code) : null,
    language_code: market?.language_code ?? null,
    search_engine: market?.search_engine ?? null,
    device: market?.device ?? null,
  };
}
