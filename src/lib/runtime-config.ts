import {
  chmodSync,
  existsSync,
  mkdirSync,
  readFileSync,
  renameSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { randomBytes } from "node:crypto";
import os from "node:os";
import path from "node:path";

export type RuntimeDefaults = {
  allowedRoots: string[];
  maxPdfBytes: number;
  maxImportBytes: number;
  maxProjectsPerUser: number;
  maxUsers: number;
};

export type RuntimeConfig = {
  version: 1;
  dataDirectory: string;
  goApiListen: string;
  listenHost: string;
  listenPort: number;
  defaults: RuntimeDefaults;
};

const defaultLimits: Omit<RuntimeDefaults, "allowedRoots"> = {
  maxPdfBytes: 104_857_600,
  maxImportBytes: 1_073_741_824,
  maxProjectsPerUser: 20,
  maxUsers: 1_000,
};
const defaultListenHost = "0.0.0.0";
const defaultListenPort = 3000;
type RuntimeConfigOptions = {
  appRoot?: string;
  environment?: RuntimeEnvironment;
  homeDirectory?: string;
};

type RuntimeEnvironment = Readonly<Record<string, string | undefined>>;

function positiveInteger(value: unknown, fallback: number) {
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : fallback;
}

function nonNegativeInteger(value: unknown, fallback: number) {
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed >= 0 ? parsed : fallback;
}

function listenPort(value: unknown, fallback: number) {
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed >= 1 && parsed <= 65_535
    ? parsed
    : fallback;
}

function listenHost(value: unknown, fallback: string) {
  if (typeof value !== "string") return fallback;
  const candidate = value.trim();
  return candidate && candidate.length <= 253 && !/[\r\n]/.test(candidate)
    ? candidate
    : fallback;
}

function configDirectory(
  environment: RuntimeEnvironment,
  homeDirectory: string,
) {
  const xdgConfigHome = environment.XDG_CONFIG_HOME?.trim();
  return xdgConfigHome
    ? path.join(xdgConfigHome, "review-hub")
    : path.join(homeDirectory, ".config", "review-hub");
}

/** Returns the only non-secret file that must exist before SQLite is opened. */
export function runtimeConfigPath(options: RuntimeConfigOptions = {}) {
  const environment = options.environment ?? process.env;
  return path.join(
    configDirectory(environment, options.homeDirectory ?? os.homedir()),
    "runtime-config.json",
  );
}

function defaultConfig(options: RuntimeConfigOptions): RuntimeConfig {
  const appRoot = options.appRoot ?? process.cwd();
  return {
    version: 1,
    dataDirectory: path.resolve(appRoot, "./data"),
    goApiListen: "127.0.0.1:39100",
    listenHost: defaultListenHost,
    listenPort: defaultListenPort,
    defaults: {
      allowedRoots: [path.resolve(appRoot)],
      maxPdfBytes: defaultLimits.maxPdfBytes,
      maxImportBytes: defaultLimits.maxImportBytes,
      maxProjectsPerUser: defaultLimits.maxProjectsPerUser,
      maxUsers: defaultLimits.maxUsers,
    },
  };
}

function parseConfig(
  value: unknown,
  options: RuntimeConfigOptions,
): RuntimeConfig {
  const fallback = defaultConfig(options);
  if (!value || typeof value !== "object") return fallback;
  const candidate = value as Partial<RuntimeConfig>;
  const defaults = candidate.defaults as Partial<RuntimeDefaults> | undefined;
  const appRoot = options.appRoot ?? process.cwd();
  const rawRoots = Array.isArray(defaults?.allowedRoots)
    ? defaults.allowedRoots.filter(
        (root): root is string => typeof root === "string",
      )
    : fallback.defaults.allowedRoots;
  return {
    version: 1,
    dataDirectory:
      typeof candidate.dataDirectory === "string" &&
      candidate.dataDirectory.trim()
        ? path.resolve(appRoot, candidate.dataDirectory)
        : fallback.dataDirectory,
    goApiListen:
      typeof candidate.goApiListen === "string" && candidate.goApiListen.trim()
        ? candidate.goApiListen.trim()
        : fallback.goApiListen,
    listenHost: listenHost(candidate.listenHost, fallback.listenHost),
    listenPort: listenPort(candidate.listenPort, fallback.listenPort),
    defaults: {
      allowedRoots: [
        ...new Set(rawRoots.map((root) => path.resolve(appRoot, root))),
      ],
      maxPdfBytes: positiveInteger(
        defaults?.maxPdfBytes,
        fallback.defaults.maxPdfBytes,
      ),
      maxImportBytes: positiveInteger(
        defaults?.maxImportBytes,
        fallback.defaults.maxImportBytes,
      ),
      maxProjectsPerUser: nonNegativeInteger(
        defaults?.maxProjectsPerUser,
        fallback.defaults.maxProjectsPerUser,
      ),
      maxUsers: positiveInteger(defaults?.maxUsers, fallback.defaults.maxUsers),
    },
  };
}

function ensurePrivateConfigDirectory(file: string) {
  mkdirSync(path.dirname(file), { recursive: true, mode: 0o700 });
  chmodSync(path.dirname(file), 0o700);
}

function writePrivateFile(file: string, content: string) {
  ensurePrivateConfigDirectory(file);
  const temporary = `${file}.${process.pid}.${randomBytes(6).toString("hex")}.tmp`;
  try {
    writeFileSync(temporary, content, { encoding: "utf8", mode: 0o600 });
    renameSync(temporary, file);
    chmodSync(file, 0o600);
  } finally {
    if (existsSync(temporary)) rmSync(temporary, { force: true });
  }
}

/** Reads the persisted bootstrap settings, creating static safe defaults once. */
export function loadRuntimeConfig(options: RuntimeConfigOptions = {}) {
  const file = runtimeConfigPath(options);
  ensurePrivateConfigDirectory(file);
  if (existsSync(file)) {
    try {
      return parseConfig(JSON.parse(readFileSync(file, "utf8")), options);
    } catch {
      throw new Error("运行配置文件无效，请修正 runtime-config.json 后重试");
    }
  }
  const configuration = defaultConfig(options);
  writePrivateFile(file, `${JSON.stringify(configuration, null, 2)}\n`);
  return configuration;
}

export function saveRuntimeConfig(
  values: RuntimeConfig,
  options: RuntimeConfigOptions = {},
) {
  const configuration = parseConfig(values, options);
  writePrivateFile(
    runtimeConfigPath(options),
    `${JSON.stringify(configuration, null, 2)}\n`,
  );
  return configuration;
}
