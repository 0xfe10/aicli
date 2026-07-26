#!/usr/bin/env node

const help = `aicli

Umbrella CLI for AI-oriented service command-line tools.

Usage:
  aicli --help
  aicli services

Commands:
  services    List registered service CLI definitions.
`;

const args = process.argv.slice(2);

if (args.length === 0 || args.includes("--help") || args.includes("-h")) {
  process.stdout.write(help);
  process.exit(0);
}

if (args.length === 1 && args[0] === "services") {
  process.stdout.write(
    JSON.stringify(
      {
        ok: true,
        data: {
          services: ["pingcode"]
        },
        meta: {
          registry: "services"
        }
      },
      null,
      2
    ) + "\n"
  );
  process.exit(0);
}

process.stdout.write(
  JSON.stringify(
    {
      ok: false,
      error: {
        code: "UNKNOWN_COMMAND",
        message: `Unknown command: ${args.join(" ")}`
      }
    },
    null,
    2
  ) + "\n"
);
process.exit(64);
