#!/usr/bin/env node

import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const envModulePath = require.resolve("@next/env");
const envModule = require(envModulePath);

// Next's CLI otherwise reads .env.local before loading next.config.mjs.
require.cache[envModulePath].exports = {
  ...envModule,
  loadEnvConfig: () => ({
    combinedEnv: process.env,
    parsedEnv: {},
    loadedEnvFiles: [],
  }),
};

require("next/dist/bin/next");
