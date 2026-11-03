// ft token broker: performs the 42 OAuth code exchange and token refresh on
// behalf of the ft CLI, so that the client secret never leaves the Worker.
//
// It is stateless: no storage, and it never logs codes or tokens.

export interface RateLimiter {
  limit(options: { key: string }): Promise<{ success: boolean }>;
}

export interface Env {
  FT_CLIENT_ID: string;
  FT_CLIENT_SECRET: string;
  // Comma-separated redirect URIs accepted by /token.
  ALLOWED_REDIRECT_URIS?: string;
  // Optional Workers rate limiting binding, keyed by client IP.
  LIMITER?: RateLimiter;
}

type Fetch = (input: string, init?: RequestInit) => Promise<Response>;

export const TOKEN_URL = "https://api.intra.42.fr/oauth/token";
const MAX_BODY_BYTES = 4096;
const MAX_FIELD_LENGTH = 512;
const DEFAULT_REDIRECT_URIS = [
  "http://localhost:8080/callback",
  "http://localhost:8081/callback",
  "http://localhost:8082/callback",
];

class HttpError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string) {
    super(code);
    this.status = status;
    this.code = code;
  }
}

export function makeHandler(fetchImpl: Fetch) {
  return async function handle(req: Request, env: Env): Promise<Response> {
    try {
      return await route(req, env, fetchImpl);
    } catch (err) {
      if (err instanceof HttpError) {
        return json(err.status, { error: err.code });
      }
      return json(502, { error: "upstream_error" });
    }
  };
}

async function route(req: Request, env: Env, fetchImpl: Fetch): Promise<Response> {
  const { pathname } = new URL(req.url);
  if (pathname !== "/token" && pathname !== "/refresh") {
    throw new HttpError(404, "not_found");
  }
  if (req.method !== "POST") {
    throw new HttpError(405, "method_not_allowed");
  }
  // CORS is closed: the broker is only for the CLI, never for browsers.
  if (req.headers.has("Origin")) {
    throw new HttpError(403, "forbidden");
  }
  if (!env.FT_CLIENT_ID || !env.FT_CLIENT_SECRET) {
    throw new HttpError(500, "broker_misconfigured");
  }
  if (env.LIMITER) {
    const ip = req.headers.get("CF-Connecting-IP") ?? "unknown";
    const { success } = await env.LIMITER.limit({ key: ip });
    if (!success) {
      throw new HttpError(429, "rate_limited");
    }
  }

  const body = await readJSON(req);
  const form = new URLSearchParams({
    client_id: env.FT_CLIENT_ID,
    client_secret: env.FT_CLIENT_SECRET,
  });

  if (pathname === "/token") {
    const redirectURI = field(body, "redirect_uri", true);
    if (!allowedRedirects(env).includes(redirectURI)) {
      throw new HttpError(400, "invalid_redirect_uri");
    }
    form.set("grant_type", "authorization_code");
    form.set("code", field(body, "code", true));
    form.set("redirect_uri", redirectURI);
    const verifier = field(body, "code_verifier", false);
    if (verifier) {
      form.set("code_verifier", verifier);
    }
  } else {
    form.set("grant_type", "refresh_token");
    form.set("refresh_token", field(body, "refresh_token", true));
  }

  const upstream = await fetchImpl(TOKEN_URL, {
    method: "POST",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
      Accept: "application/json",
    },
    body: form.toString(),
  });
  const text = await upstream.text();
  try {
    JSON.parse(text);
  } catch {
    throw new HttpError(502, "upstream_error");
  }
  return new Response(text, { status: upstream.status, headers: responseHeaders() });
}

async function readJSON(req: Request): Promise<Record<string, unknown>> {
  if (!(req.headers.get("Content-Type") ?? "").startsWith("application/json")) {
    throw new HttpError(415, "unsupported_media_type");
  }
  const declared = Number(req.headers.get("Content-Length") ?? "0");
  if (declared > MAX_BODY_BYTES) {
    throw new HttpError(413, "payload_too_large");
  }
  const text = await req.text();
  if (new TextEncoder().encode(text).length > MAX_BODY_BYTES) {
    throw new HttpError(413, "payload_too_large");
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    throw new HttpError(400, "invalid_json");
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    throw new HttpError(400, "invalid_json");
  }
  return parsed as Record<string, unknown>;
}

function field(body: Record<string, unknown>, name: string, required: boolean): string {
  const v = body[name];
  if (v === undefined || v === "") {
    if (required) {
      throw new HttpError(400, `missing_${name}`);
    }
    return "";
  }
  if (typeof v !== "string" || v.length > MAX_FIELD_LENGTH) {
    throw new HttpError(400, `invalid_${name}`);
  }
  return v;
}

function allowedRedirects(env: Env): string[] {
  if (!env.ALLOWED_REDIRECT_URIS) {
    return DEFAULT_REDIRECT_URIS;
  }
  return env.ALLOWED_REDIRECT_URIS.split(",").map((s) => s.trim()).filter(Boolean);
}

function responseHeaders(): HeadersInit {
  return {
    "Content-Type": "application/json",
    "Cache-Control": "no-store",
    "X-Content-Type-Options": "nosniff",
  };
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: responseHeaders() });
}
