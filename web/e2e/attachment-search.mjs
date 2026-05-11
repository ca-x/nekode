import { chromium, expect } from "@playwright/test";
import { spawn } from "node:child_process";
import { createServer } from "node:net";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = path.resolve(webRoot, "..");

async function freePort() {
  const server = createServer();
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  await new Promise((resolve) => server.close(resolve));
  return address.port;
}

async function waitForServer(url, child, logs) {
  const deadline = Date.now() + 60_000;
  let lastError;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(`nekode server exited with ${child.exitCode}\n${logs.join("")}`);
    }
    try {
      const response = await fetch(`${url}/health`);
      if (response.ok) return;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`nekode server did not become healthy: ${lastError?.message ?? "timeout"}\n${logs.join("")}`);
}

async function startNekode() {
  const httpPort = await freePort();
  const tmpDir = await mkdtemp(path.join(os.tmpdir(), "nekode-e2e-"));
  const baseURL = `http://127.0.0.1:${httpPort}`;
  const env = {
    ...process.env,
    NEKODE_ADDR: `127.0.0.1:${httpPort}`,
    NEKODE_BASE_URL: baseURL,
    NEKODE_DAEMON_RPC_URL: baseURL,
    NEKODE_DATA_DIR: tmpDir,
    NEKODE_DB_TYPE: "sqlite",
    NEKODE_DB_DSN: path.join(tmpDir, "nekode.db"),
    NEKODE_CACHE_DRIVER: "none",
    NEKODE_WEB_DIST_DIR: path.join(webRoot, "dist"),
    NEKODE_BOOTSTRAP_ADMIN_USERNAME: "e2e-admin",
    NEKODE_BOOTSTRAP_ADMIN_PASSWORD: "e2e-password",
    NEKODE_BOOTSTRAP_ADMIN_NAME: "E2E Admin"
  };
  const logs = [];
  const child = spawn("go", ["run", "./cmd/nekode", "serve"], {
    cwd: repoRoot,
    env,
    stdio: ["ignore", "pipe", "pipe"]
  });
  child.stdout.on("data", (chunk) => logs.push(chunk.toString()));
  child.stderr.on("data", (chunk) => logs.push(chunk.toString()));
  try {
    await waitForServer(baseURL, child, logs);
  } catch (error) {
    if (child.exitCode === null) child.kill("SIGTERM");
    await rm(tmpDir, { recursive: true, force: true });
    throw error;
  }
  return {
    baseURL,
    stop: async () => {
      if (child.exitCode === null) child.kill("SIGTERM");
      await new Promise((resolve) => {
        if (child.exitCode !== null) return resolve();
        child.once("exit", resolve);
        setTimeout(resolve, 5_000);
      });
      await rm(tmpDir, { recursive: true, force: true });
    }
  };
}

async function run() {
  const server = await startNekode();
  let browser;
  try {
    browser = await chromium.launch();
    const page = await browser.newPage();
    const filename = `preview-${Date.now()}.html`;
    const textFilename = `notes-${Date.now()}.txt`;
    const textContent = `plain text inline preview ${Date.now()}`;
    const messageText = `attachment e2e ${Date.now()}`;

    await page.goto(server.baseURL);
    await page.getByLabel("Username").fill("e2e-admin");
    await page.getByLabel("Password").fill("e2e-password");
    await page.getByRole("button", { name: /Sign In|Create Admin/ }).click();
    await expect(page.getByLabel("Current target")).toBeVisible();

    await page.getByRole("button", { name: /^Messages$/ }).click();
    await page.locator('input[type="file"]').setInputFiles([
      {
        name: filename,
        mimeType: "text/html",
        buffer: Buffer.from("<strong>attachment search e2e</strong>")
      },
      {
        name: textFilename,
        mimeType: "text/plain",
        buffer: Buffer.from(textContent)
      }
    ]);
    await expect(page.getByText(filename)).toBeVisible();
    await expect(page.getByText(textFilename)).toBeVisible();
    await page.getByLabel("Message content").fill(messageText);
    await page.getByRole("button", { name: /^Send$/ }).click();

    const bubble = page.locator(".message-bubble").filter({ hasText: messageText }).last();
    await expect(bubble).toBeVisible();
    await expect(bubble).toContainText(textContent);
    await bubble.getByRole("button", { name: `Preview ${textFilename}` }).click();
    await expect(page.getByRole("dialog")).toContainText(textContent);
    await page.getByRole("button", { name: "Close preview" }).click();
    await bubble.getByRole("button", { name: /^Save$/ }).click();
    await expect(bubble.getByRole("button", { name: /^Saved$/ })).toBeVisible();

    await page.getByLabel("Search messages").fill(filename);
    await page.getByLabel("Only messages with attachments").check();
    await page.getByRole("button", { name: /^Search$/ }).click();
    await expect(page.getByLabel("Message search results")).toContainText(filename);

    await page.getByLabel("Search saved messages").fill(filename);
    await page.getByLabel("Only saved messages with attachments").check();
    await page.getByRole("button", { name: /^Find$/ }).click();
    await expect(page.getByLabel("Saved message search results")).toContainText(filename);
  } finally {
    await browser?.close();
    await server.stop();
  }
}

run().catch((error) => {
  console.error(error);
  process.exit(1);
});
