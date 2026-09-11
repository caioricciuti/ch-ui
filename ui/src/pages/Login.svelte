<script lang="ts">
  import { onMount } from "svelte";
  import { listConnections, getAuthConfig } from "../lib/api/auth";
  import { login, getError } from "../lib/stores/session.svelte";
  import type { Connection } from "../lib/types/api";
  import Combobox from "../lib/components/common/Combobox.svelte";
  import Spinner from "../lib/components/common/Spinner.svelte";
  import Sheet from "../lib/components/common/Sheet.svelte";
  import {
    Wifi,
    WifiOff,
    Database,
    ShieldCheck,
    ArrowRight,
    AlertTriangle,
    BookOpen,
    ExternalLink,
  } from "lucide-svelte";
  import logo from "../assets/logo.png";

  let connections = $state<Connection[]>([]);
  let loadingConnections = $state(true);
  let selectedId = $state("");
  let username = $state("");
  let password = $state("");
  let submitting = $state(false);
  let localError = $state<string | null>(null);
  let showSetupSheet = $state(false);
  let setupClickHouseURL = $state("http://localhost:8123");
  let setupConnectionName = $state("Local ClickHouse");

  let ssoEnabled = $state(false);
  let ssoLoginUrl = $state("/api/auth/oidc/login");
  let ssoError = $state<string | null>(null);

  type LoginErrorKind = "auth" | "connection" | "rateLimit" | "generic";

  type LoginHelp = {
    title: string;
    detail?: string;
  };

  function classifyLoginError(message: string | null): LoginErrorKind | null {
    if (!message) return null;
    const normalized = message.toLowerCase();
    if (normalized.includes("invalid credentials") || normalized.includes("authentication failed")) {
      return "auth";
    }
    if (normalized.includes("connection offline") || normalized.includes("unreachable") || normalized.includes("tunnel")) {
      return "connection";
    }
    if (normalized.includes("too many login attempts") || normalized.includes("retry in")) {
      return "rateLimit";
    }
    return "generic";
  }

  function buildLoginHelp(kind: LoginErrorKind | null): LoginHelp | null {
    if (!kind) return null;
    if (kind === "auth") {
      return {
        title: "Authentication failed",
        detail: "Verify username/password and selected ClickHouse connection.",
      };
    }
    if (kind === "connection") {
      return {
        title: "Connection unavailable",
        detail: "Start the connector/agent for this connection, then retry.",
      };
    }
    if (kind === "rateLimit") {
      return {
        title: "Login temporarily blocked",
        detail: "Wait for the retry window and try again.",
      };
    }
    return { title: "Login failed" };
  }

  onMount(async () => {
    try {
      const params = new URLSearchParams(window.location.search);
      const err = params.get("sso_error");
      if (err) ssoError = err;
    } catch {}

    getAuthConfig().then((cfg) => {
      ssoEnabled = cfg.oidc_enabled;
      if (cfg.oidc_login_url) ssoLoginUrl = cfg.oidc_login_url;
    });

    try {
      connections = await listConnections();
      if (connections.length === 1) {
        selectedId = connections[0].id;
      }
    } catch (e: any) {
      localError = e.message || "Failed to load connections";
    } finally {
      loadingConnections = false;
    }
  });

  async function handleSubmit(e: SubmitEvent) {
    e.preventDefault();
    if (!selectedId || !username) {
      localError = "Connection and username are required";
      return;
    }
    const selected = connections.find((c) => c.id === selectedId);
    if (selected && !selected.online) {
      localError = `Connection "${selected.name}" is offline. Bring it online and retry.`;
      return;
    }

    localError = null;
    submitting = true;
    try {
      await login(selectedId, username, password);
    } catch (e: any) {
      localError = e.message || "Login failed";
    } finally {
      submitting = false;
    }
  }

  function shellQuote(value: string): string {
    return `'${value.replace(/'/g, `'\"'\"'`)}'`;
  }

  const error = $derived(localError || getError());
  const selectedConnection = $derived(connections.find((c) => c.id === selectedId) || null);
  const canSubmit = $derived(Boolean(selectedId && username && (selectedConnection ? selectedConnection.online : false)));
  const errorKind = $derived(classifyLoginError(error));
  const loginHelp = $derived(buildLoginHelp(errorKind));
  const showSetupRecoveryCTA = $derived(errorKind === "connection" || errorKind === "rateLimit");
  const quickHelpURL = "https://github.com/caioricciuti/ch-ui#cant-login";
  const cantLoginDocsURL = "https://github.com/caioricciuti/ch-ui/blob/main/docs/cant-login.md";
  const dockerDocsURL = "https://github.com/caioricciuti/ch-ui#quick-start-docker";
  const normalizedSetupURL = $derived(setupClickHouseURL.trim() || "http://localhost:8123");
  const normalizedSetupConnectionName = $derived(setupConnectionName.trim() || "Local ClickHouse");
  const localCommand = $derived(
    `ch-ui server --clickhouse-url ${shellQuote(normalizedSetupURL)} --connection-name ${shellQuote(normalizedSetupConnectionName)}`
  );
  const localCommandWithBinary = $derived(
    `./ch-ui server --clickhouse-url ${shellQuote(normalizedSetupURL)} --connection-name ${shellQuote(normalizedSetupConnectionName)}`
  );
  const dockerCommand = $derived(
    `docker run --rm -p 3488:3488 -v ch-ui-data:/app/data -e CLICKHOUSE_URL=${shellQuote(normalizedSetupURL)} -e CONNECTION_NAME=${shellQuote(normalizedSetupConnectionName)} ghcr.io/caioricciuti/ch-ui:latest`
  );
</script>

<div class="relative flex h-full w-full overflow-hidden bg-canvas text-fg">
  <!-- Backdrop: 40 px grid on the theme's subtle edge color, plus one soft orange halo behind the form -->
  <div
    class="pointer-events-none absolute inset-0 opacity-[0.35]"
    style="background-image: linear-gradient(var(--edge-subtle) 1px, transparent 1px), linear-gradient(90deg, var(--edge-subtle) 1px, transparent 1px); background-size: 40px 40px;"
  ></div>
  <div class="pointer-events-none absolute left-1/2 top-1/4 h-[520px] w-[520px] -translate-x-1/2 rounded-full bg-accent/12 blur-[140px]"></div>
  <!-- Sign-in form, centered; the page is the form and nothing else. -->
  <main class="relative flex flex-1 flex-col items-center justify-center overflow-y-auto px-6 py-12">
    <div class="w-full max-w-[400px]">
      <div class="mb-8 flex items-center gap-3">
        <img src={logo} alt="CH-UI" class="h-9 w-9 rounded-lg" />
        <span class="text-base font-semibold tracking-tight">CH-UI</span>
      </div>

      <div class="flex items-start justify-between gap-4">
        <div>
          <h1 class="text-[26px] font-semibold tracking-[-0.02em]">Sign in</h1>
          <p class="mt-1 text-[14px] text-fg-3">Connect to your ClickHouse instance</p>
        </div>
        <button
          type="button"
          class="inline-flex h-8 shrink-0 items-center gap-1.5 rounded-md px-2.5 text-[13px] font-medium text-fg-3 transition-colors hover:bg-hover hover:text-fg"
          onclick={() => (showSetupSheet = true)}
        >
          <BookOpen size={14} />
          Can't login?
        </button>
      </div>

      <p class="mt-6 inline-flex items-center gap-1.5 text-[12px] text-fg-3">
        <ShieldCheck size={13} class="text-success" />
        Credentials are sent straight to your ClickHouse server.
      </p>

      {#if loadingConnections}
        <div class="mt-10 flex items-center gap-3 text-[13px] text-fg-3">
          <Spinner size="sm" />
          Discovering connections…
        </div>
      {:else if connections.length === 0}
        <div class="mt-10">
          <div class="flex h-11 w-11 items-center justify-center rounded-md bg-surface-2 text-fg-3">
            <Database size={20} />
          </div>
          <h3 class="mt-4 text-[15px] font-semibold">No connections configured</h3>
          <p class="mt-1 text-[13px] leading-relaxed text-fg-3">
            No local connection is ready yet. Open setup and restart CH-UI with the correct URL.
          </p>
          <button
            type="button"
            class="mt-5 inline-flex h-9 items-center gap-2 rounded-md bg-accent px-4 text-[13px] font-medium text-accent-fg hover:brightness-110"
            onclick={() => (showSetupSheet = true)}
          >
            <BookOpen size={14} />
            Open setup guide
          </button>
        </div>
      {:else}
        <form onsubmit={handleSubmit} class="mt-8 space-y-5">
          <div>
            <label class="mb-1.5 block text-[13px] font-medium text-fg-2" for="connection">Connection</label>
            <Combobox
              size="lg"
              options={connections.map((conn) => ({
                value: conn.id,
                label: conn.name,
                hint: conn.online ? "Online" : "Offline",
                keywords: `${conn.name} ${conn.id}`,
              }))}
              value={selectedId}
              placeholder="Select a connection..."
              onChange={(id) => (selectedId = id)}
            />
            {#if selectedId}
              {@const selected = connections.find((c) => c.id === selectedId)}
              {#if selected}
                <p class="mt-1.5 inline-flex items-center gap-1.5 text-[12px] {selected.online ? 'text-success' : 'text-warning'}">
                  {#if selected.online}
                    <Wifi size={12} /> Connected
                  {:else}
                    <WifiOff size={12} /> Unreachable
                  {/if}
                </p>
              {/if}
            {/if}
          </div>

          <div>
            <label class="mb-1.5 block text-[13px] font-medium text-fg-2" for="username">Username</label>
            <input
              id="username"
              type="text"
              bind:value={username}
              placeholder="default"
              autocomplete="username"
              class="h-10 w-full rounded-md border border-edge bg-surface px-3 text-[14px] text-fg placeholder:text-fg-4 transition-colors hover:border-edge-strong focus:border-accent focus:outline-none"
            />
          </div>

          <div>
            <label class="mb-1.5 block text-[13px] font-medium text-fg-2" for="password">Password</label>
            <input
              id="password"
              type="password"
              bind:value={password}
              placeholder="Optional"
              autocomplete="current-password"
              class="h-10 w-full rounded-md border border-edge bg-surface px-3 text-[14px] text-fg placeholder:text-fg-4 transition-colors hover:border-edge-strong focus:border-accent focus:outline-none"
            />
          </div>

          {#if error}
            <div class="rounded-md border border-danger/30 bg-danger-soft px-3.5 py-3 text-[13px]" role="alert">
              <p class="inline-flex items-center gap-1.5 font-semibold text-danger">
                <AlertTriangle size={14} />
                {loginHelp?.title ?? "Login failed"}
              </p>
              <p class="mt-1 text-fg-2">{error}</p>
              {#if loginHelp?.detail}
                <p class="mt-1 text-fg-3">{loginHelp.detail}</p>
              {/if}
              {#if showSetupRecoveryCTA}
                <button
                  type="button"
                  class="mt-3 inline-flex h-8 items-center gap-1.5 rounded-md border border-edge bg-surface px-3 text-[12px] font-medium text-fg-2 hover:bg-hover hover:text-fg"
                  onclick={() => (showSetupSheet = true)}
                >
                  <BookOpen size={13} />
                  Open setup guide
                </button>
                {#if errorKind === "rateLimit"}
                  <p class="mt-2 text-[12px] text-fg-3">
                    If retries came from the wrong local URL, update setup and restart CH-UI before trying again.
                  </p>
                {/if}
              {/if}
            </div>
          {/if}

          <button
            type="submit"
            class="inline-flex h-10 w-full items-center justify-center gap-2 rounded-md bg-accent text-[14px] font-semibold text-accent-fg transition-[filter] hover:brightness-110 active:brightness-95 disabled:opacity-50 disabled:pointer-events-none"
            disabled={!canSubmit || submitting}
          >
            {#if submitting}
              <Spinner size="sm" />
            {/if}
            Connect
            <ArrowRight size={15} />
          </button>

          {#if ssoEnabled}
            <div class="flex items-center gap-3 text-[12px] text-fg-4">
              <span class="h-px flex-1 bg-edge"></span>
              or
              <span class="h-px flex-1 bg-edge"></span>
            </div>
            {#if ssoError}
              <div class="rounded-md bg-danger-soft px-3 py-2 text-[12px] text-danger">{ssoError}</div>
            {/if}
            <button
              type="button"
              class="inline-flex h-10 w-full items-center justify-center gap-2 rounded-md border border-edge bg-surface text-[14px] font-medium text-fg transition-colors hover:bg-hover"
              onclick={() => (window.location.href = ssoLoginUrl)}
            >
              <ShieldCheck size={15} />
              Sign in with SSO
            </button>
          {/if}
        </form>
      {/if}

      <Sheet
        open={showSetupSheet}
        title="Can't login? Setup guide"
        size="lg"
        onclose={() => (showSetupSheet = false)}
      >
        <div class="space-y-5 text-[13px]">
          <p class="text-fg-2">
            Set the URL and name, run one command, restart CH-UI, then return to sign in.
          </p>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label class="mb-1.5 block text-[13px] font-medium text-fg-2" for="sheet-clickhouse-url">ClickHouse URL</label>
              <input
                id="sheet-clickhouse-url"
                type="url"
                bind:value={setupClickHouseURL}
                placeholder="http://localhost:8123"
                class="ds-input"
              />
            </div>
            <div>
              <label class="mb-1.5 block text-[13px] font-medium text-fg-2" for="sheet-connection-name">Connection name</label>
              <input
                id="sheet-connection-name"
                type="text"
                bind:value={setupConnectionName}
                placeholder="Local ClickHouse"
                class="ds-input"
              />
            </div>
          </div>
          <ol class="list-decimal space-y-1.5 pl-5 text-fg-2">
            <li>Stop any running <code class="rounded bg-surface-2 px-1 py-0.5 font-mono text-[12px]">ch-ui server</code> process.</li>
            <li>Run one setup command with your URL and connection name.</li>
            <li>Open <code class="rounded bg-surface-2 px-1 py-0.5 font-mono text-[12px]">http://localhost:3488</code> and sign in with your ClickHouse credentials.</li>
          </ol>

          {#each [
            ['Run with globally installed ch-ui', localCommand],
            ['Run with local binary', localCommandWithBinary],
            ['Run with Docker', dockerCommand],
          ] as [title, cmd]}
            <div>
              <p class="mb-1.5 text-[12px] font-medium text-fg-3">{title}</p>
              <pre class="overflow-x-auto rounded-md bg-surface-2 p-3 font-mono text-[12px] leading-relaxed text-fg">{cmd}</pre>
            </div>
          {/each}

          <div class="flex flex-wrap gap-4">
            <a href={quickHelpURL} target="_blank" rel="noopener" class="inline-flex items-center gap-1 text-accent hover:underline">
              Can't login? Quick path <ExternalLink size={12} />
            </a>
            <a href={cantLoginDocsURL} target="_blank" rel="noopener" class="inline-flex items-center gap-1 text-accent hover:underline">
              Full Can't login doc <ExternalLink size={12} />
            </a>
            <a href={dockerDocsURL} target="_blank" rel="noopener" class="inline-flex items-center gap-1 text-accent hover:underline">
              Docker Quick Start <ExternalLink size={12} />
            </a>
          </div>

          <p class="text-[12px] text-fg-3">
            Setup never stores ClickHouse credentials and commands never include passwords.
          </p>
        </div>
      </Sheet>

    </div>
  </main>

  <div class="absolute bottom-6 right-6 z-10 flex items-center gap-5 text-[13px] text-fg-3">
    <a href="https://github.com/caioricciuti/ch-ui" target="_blank" rel="noopener" class="transition-colors hover:text-fg">GitHub</a>
    <a href={cantLoginDocsURL} target="_blank" rel="noopener" class="transition-colors hover:text-fg">Docs</a>
  </div>
</div>
