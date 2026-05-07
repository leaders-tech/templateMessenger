import { request } from "node:http";

const req = request(
  { host: "127.0.0.1", port: process.env.PORT || 8080, path: "/healthz", timeout: 2000 },
  (res) => {
    process.exit(res.statusCode === 200 ? 0 : 1);
  }
);

req.on("error", () => process.exit(1));
req.end();

