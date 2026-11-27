export const googleOAuthCallbackPath = "/seo/api/integrations/oauth/callback/google";
export const googleOAuthResultKey = "google_oauth";

export type OAuthIntegrationProvider = "google" | "bing";
export const integrationOAuthResultKey = "integration_oauth";

export function integrationOAuthCallbackPath(provider: OAuthIntegrationProvider) {
  return `/seo/api/integrations/oauth/callback/${provider}`;
}
