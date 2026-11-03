import assert from "node:assert/strict";
import { test } from "node:test";

import { makeHandler, TOKEN_URL, type Env } from "../src/handler.ts";

const env: Env = { FT_CLIENT_ID: "cid", FT_CLIENT_SECRET: "s3cret" };

function post(path: string, body: unknown, headers: Record<string, string> = {}): Request {
  return new Request("https://broker.example" + path, {
    method: "POST",
    headers: { "Content-Type": "application/json", ...headers },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
}

// upstream records the last request sent to 42 and replies with reply.
function upstream(reply: Response) {
  const calls: { url: string; form: URLSearchParams }[] = [];
  const handle = makeHandler(async (url, init) => {
    calls.push({ url, form: new URLSearchParams(String(init?.body)) });
    return reply;
  });
  return { handle, calls };
}

const tokenReply = () =>
  new Response(JSON.stringify({ access_token: "at", refresh_token: "rt", expires_in: 7200 }), { status: 200 });

test("/token exchanges the code with the secret", async () => {
  const { handle, calls } = upstream(tokenReply());
  const res = await handle(
    post("/token", { code: "c", redirect_uri: "http://localhost:8081/callback", code_verifier: "v" }),
    env,
  );
  assert.equal(res.status, 200);
  assert.equal(res.headers.get("Cache-Control"), "no-store");
  assert.equal((await res.json()).access_token, "at");
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, TOKEN_URL);
  assert.deepEqual(Object.fromEntries(calls[0].form), {
    client_id: "cid",
    client_secret: "s3cret",
    grant_type: "authorization_code",
    code: "c",
    redirect_uri: "http://localhost:8081/callback",
    code_verifier: "v",
  });
});

test("/refresh uses the refresh_token grant", async () => {
  const { handle, calls } = upstream(tokenReply());
  const res = await handle(post("/refresh", { refresh_token: "rt" }), env);
  assert.equal(res.status, 200);
  assert.equal(calls[0].form.get("grant_type"), "refresh_token");
  assert.equal(calls[0].form.get("refresh_token"), "rt");
});

test("upstream errors are relayed without the secret", async () => {
  const { handle } = upstream(new Response(JSON.stringify({ error: "invalid_grant" }), { status: 400 }));
  const res = await handle(post("/refresh", { refresh_token: "bad" }), env);
  assert.equal(res.status, 400);
  const text = await res.text();
  assert.match(text, /invalid_grant/);
  assert.doesNotMatch(text, /s3cret/);
});

test("rejects bad requests before calling 42", async (t) => {
  const cases: [string, Request, number][] = [
    ["unknown route", post("/other", {}), 404],
    ["GET", new Request("https://broker.example/token"), 405],
    ["browser origin", post("/refresh", { refresh_token: "x" }, { Origin: "https://evil.example" }), 403],
    ["wrong content type", post("/refresh", "refresh_token=x", { "Content-Type": "text/plain" }), 415],
    ["invalid json", post("/refresh", "{"), 400],
    ["json array", post("/refresh", "[]"), 400],
    ["missing field", post("/refresh", {}), 400],
    ["non-string field", post("/refresh", { refresh_token: 1 }), 400],
    ["field too long", post("/refresh", { refresh_token: "x".repeat(600) }), 400],
    ["body too large", post("/refresh", { refresh_token: "x", pad: "y".repeat(5000) }), 413],
    ["unregistered redirect", post("/token", { code: "c", redirect_uri: "https://evil.example/cb" }), 400],
  ];
  for (const [name, req, status] of cases) {
    await t.test(name, async () => {
      const { handle, calls } = upstream(tokenReply());
      const res = await handle(req, env);
      assert.equal(res.status, status);
      assert.equal(calls.length, 0);
    });
  }
});

test("rate limiter blocks by client IP", async () => {
  const keys: string[] = [];
  const limited: Env = {
    ...env,
    LIMITER: {
      async limit({ key }) {
        keys.push(key);
        return { success: false };
      },
    },
  };
  const { handle, calls } = upstream(tokenReply());
  const res = await handle(post("/refresh", { refresh_token: "x" }, { "CF-Connecting-IP": "1.2.3.4" }), limited);
  assert.equal(res.status, 429);
  assert.deepEqual(keys, ["1.2.3.4"]);
  assert.equal(calls.length, 0);
});

test("missing secret is reported as misconfiguration", async () => {
  const { handle } = upstream(tokenReply());
  const res = await handle(post("/refresh", { refresh_token: "x" }), { FT_CLIENT_ID: "cid", FT_CLIENT_SECRET: "" });
  assert.equal(res.status, 500);
});

test("non-JSON upstream response becomes 502", async () => {
  const { handle } = upstream(new Response("<html>oops</html>", { status: 500 }));
  const res = await handle(post("/refresh", { refresh_token: "x" }), env);
  assert.equal(res.status, 502);
});
