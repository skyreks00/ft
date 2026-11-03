import { makeHandler, type Env } from "./handler.ts";

const handle = makeHandler((input, init) => fetch(input, init));

export default {
  fetch(req: Request, env: Env): Promise<Response> {
    return handle(req, env);
  },
};
