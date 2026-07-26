#!/usr/bin/env node

import { spawnSync } from "node:child_process";

const help = `aicli

Umbrella CLI for AI-oriented service command-line tools.

Usage:
  aicli --help
  aicli services
  aicli pingcode restish setup --base-url <url>
  aicli pingcode project list [--identifier <id>] [--page-size <n>] [--page-index <n>]
  aicli pingcode work-item search --project-id <id> [--page-size <n>] [--page-index <n>]

Commands:
  services    List registered service CLI definitions.
  pingcode    Experimental PingCode commands backed by Restish.
`;

const args = process.argv.slice(2);

function writeJson(payload) {
  process.stdout.write(JSON.stringify(payload, null, 2) + "\n");
}

function fail(code, message, exitCode = 64, meta = undefined) {
  writeJson({
    ok: false,
    error: { code, message },
    ...(meta ? { meta } : {})
  });
  process.exit(exitCode);
}

function redact(value) {
  return value
    .replace(/(Authorization:\s*Bearer\s+)[A-Za-z0-9._-]+/gi, "$1***")
    .replace(/(Bearer\s+)[A-Za-z0-9._-]+/g, "$1***")
    .replace(/("?(access_token|refresh_token|client_secret|code)"?\s*[:=]\s*"?)[^"&\s,}]+/gi, "$1***");
}

function parseOptions(values) {
  const options = {};
  const positional = [];

  for (let index = 0; index < values.length; index += 1) {
    const value = values[index];
    if (!value.startsWith("--")) {
      positional.push(value);
      continue;
    }

    const [rawKey, inlineValue] = value.slice(2).split("=", 2);
    const key = rawKey.replace(/-([a-z])/g, (_, char) => char.toUpperCase());

    if (inlineValue !== undefined) {
      options[key] = inlineValue;
      continue;
    }

    const next = values[index + 1];
    if (next === undefined || next.startsWith("--")) {
      options[key] = true;
      continue;
    }

    options[key] = next;
    index += 1;
  }

  return { options, positional };
}

function requiredOption(options, key, flag) {
  if (!options[key] || options[key] === true) {
    fail("MISSING_OPTION", `Missing required option: ${flag}`);
  }

  return options[key];
}

function addQuery(restishArgs, key, value) {
  if (value === undefined || value === true) {
    return;
  }

  restishArgs.push("--rsh-query", `${key}=${value}`);
}

function parseRestishBody(stdout) {
  const text = stdout.trim();
  if (text.length === 0) {
    return null;
  }

  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function runRestish(restishArgs) {
  const result = spawnSync("restish", restishArgs, {
    encoding: "utf8"
  });

  if (result.error?.code === "ENOENT") {
    fail(
      "RESTISH_NOT_FOUND",
      "Restish is not installed or is not available on PATH. Install it with Linuxbrew: brew install restish",
      69
    );
  }

  if (result.status !== 0) {
    fail("RESTISH_COMMAND_FAILED", "Restish command failed.", result.status || 1, {
      restish: {
        args: restishArgs,
        stdout: redact(result.stdout.trim()),
        stderr: redact(result.stderr.trim())
      }
    });
  }

  return parseRestishBody(result.stdout);
}

function handleServices() {
  writeJson({
    ok: true,
    data: {
      services: ["pingcode"]
    },
    meta: {
      registry: "services"
    }
  });
}

function handlePingCode(values) {
  const [area, action, ...rest] = values;

  if (area === "restish" && action === "setup") {
    const { options } = parseOptions(rest);
    const baseUrl = requiredOption(options, "baseUrl", "--base-url");
    const body = runRestish(["api", "connect", "pingcode", baseUrl, "--no-discover", "--yes"]);

    writeJson({
      ok: true,
      data: {
        profile: "pingcode",
        baseUrl,
        restish: body
      },
      meta: {
        next: "Configure PingCode auth in Restish, then run: aicli pingcode project list"
      }
    });
    return;
  }

  if (area === "project" && action === "list") {
    const { options } = parseOptions(rest);
    const restishArgs = ["get", "pingcode/v1/project/projects", "-o", "json", "--rsh-print", "b"];

    addQuery(restishArgs, "identifier", options.identifier);
    addQuery(restishArgs, "page_size", options.pageSize);
    addQuery(restishArgs, "page_index", options.pageIndex);

    writeJson({
      ok: true,
      data: runRestish(restishArgs),
      meta: {
        service: "pingcode",
        command: "project list",
        transport: "restish"
      }
    });
    return;
  }

  if (area === "work-item" && action === "search") {
    const { options } = parseOptions(rest);
    const projectId = requiredOption(options, "projectId", "--project-id");
    const restishArgs = ["get", "pingcode/v1/project/work_items", "-o", "json", "--rsh-print", "b"];

    addQuery(restishArgs, "project_id", projectId);
    addQuery(restishArgs, "page_size", options.pageSize);
    addQuery(restishArgs, "page_index", options.pageIndex);

    writeJson({
      ok: true,
      data: runRestish(restishArgs),
      meta: {
        service: "pingcode",
        command: "work-item search",
        transport: "restish"
      }
    });
    return;
  }

  fail("UNKNOWN_COMMAND", `Unknown PingCode command: ${values.join(" ")}`);
}

if (args.length === 0 || args.includes("--help") || args.includes("-h")) {
  process.stdout.write(help);
  process.exit(0);
}

if (args.length === 1 && args[0] === "services") {
  handleServices();
  process.exit(0);
}

if (args[0] === "pingcode") {
  handlePingCode(args.slice(1));
  process.exit(0);
}

fail("UNKNOWN_COMMAND", `Unknown command: ${args.join(" ")}`);
