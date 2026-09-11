<script lang="ts">
	import { formatDate, formatBytes } from '../lib/utils/format';
	import { onMount } from 'svelte';
	import {
		Table2,
		Search,
		Plus,
		Trash2,
		PanelRightOpen,
		Info,
		X
	} from 'lucide-svelte';
	import Spinner from '../lib/components/common/Spinner.svelte';
	import Combobox from '../lib/components/common/Combobox.svelte';
	import type { ComboboxOption } from '../lib/components/common/Combobox.svelte';
	import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte';
	import Sheet from '../lib/components/common/Sheet.svelte';
	import HelpTip from '../lib/components/common/HelpTip.svelte';
	import MiniTrendChart from '../lib/components/common/MiniTrendChart.svelte';
	import PageHeader from '../lib/components/common/PageHeader.svelte';
	import PageBody from '../lib/components/common/PageBody.svelte';
	import Button from '../lib/components/common/Button.svelte';
	import Badge from '../lib/components/common/Badge.svelte';
	import Panel from '../lib/components/common/Panel.svelte';
	import Stat from '../lib/components/common/Stat.svelte';
	import EmptyState from '../lib/components/common/EmptyState.svelte';
	import Input from '../lib/components/common/Input.svelte';
	import Textarea from '../lib/components/common/Textarea.svelte';
	import DataTable, { type DataColumn } from '../lib/components/common/DataTable.svelte';
	import AccessSection from '../lib/components/governance/AccessSection.svelte';
	import IncidentsSection from '../lib/components/governance/IncidentsSection.svelte';
	import PoliciesSection from '../lib/components/governance/PoliciesSection.svelte';
	import AlertsSection from '../lib/components/governance/AlertsSection.svelte';
	import AuditLogSection from '../lib/components/governance/AuditLogSection.svelte';
	import SettingsSection from '../lib/components/governance/SettingsSection.svelte';
	import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte';
	import {
		fetchOverview,
		fetchDatabases,
		fetchTables,
		fetchTableDetail,
		fetchQueryLog,
		fetchTableNotes,
		createTableNote as apiCreateTableNote,
		deleteObjectNote as apiDeleteObjectNote,
		triggerSync,
		fetchGovernanceSettings,
		updateGovernanceSettings
	} from '../lib/api/governance';
	import { getSection, setSection } from '../lib/stores/nav.svelte';
	import type {
		GovernanceOverview,
		GovDatabase,
		GovTable,
		GovColumn,
		QueryLogEntry,
		GovernanceObjectComment,
		SyncState,
		GovernanceSettings
	} from '../lib/types/governance';

	// State
	type GovernanceTab = 'dashboard' | 'tables' | 'queries' | 'access' | 'incidents' | 'policies' | 'alerts' | 'auditlog' | 'settings';
	const governanceTabs: Array<{ id: GovernanceTab; label: string }> = [
		{ id: 'dashboard', label: 'Dashboard' },
		{ id: 'tables', label: 'Tables' },
		{ id: 'queries', label: 'Query Audit' },
		{ id: 'access', label: 'Access' },
		{ id: 'incidents', label: 'Incidents' },
		{ id: 'policies', label: 'Policies' },
		{ id: 'alerts', label: 'Alerts' },
		{ id: 'auditlog', label: 'Audit Log' },
		{ id: 'settings', label: 'Settings' },
	];

	let activeTab = $state<GovernanceTab>('dashboard');
	let loading = $state<boolean>(false);
	let syncing = $state<boolean>(false);

	// Sync settings (opt-in toggle + upgrade banner)
	let govSettings = $state<GovernanceSettings | null>(null);
	let govToggleSaving = $state<boolean>(false);
	// Dashboard data
	let overview = $state<GovernanceOverview | null>(null);

	// Tables data
	let databases = $state<GovDatabase[]>([]);
	let tables = $state<GovTable[]>([]);
	let selectedDatabase = $state<string>('');
	let tableSearchQuery = $state<string>('');
	let tableDetailSheetOpen = $state<boolean>(false);
	let selectedTable = $state<GovTable | null>(null);
	let selectedTableColumns = $state<GovColumn[]>([]);
	let tableDetailLoading = $state<boolean>(false);
	let tableNotes = $state<GovernanceObjectComment[]>([]);
	let tableNoteDraft = $state<string>('');

	// Query Audit data
	let queryLog = $state<QueryLogEntry[]>([]);
	let queryUserFilter = $state<string>('');
	let queryLimit = $state<number>(100);
	let queryDetailSheetOpen = $state<boolean>(false);
	let selectedQuery = $state<QueryLogEntry | null>(null);

	// Access data
	// Policies data
	// ── Alerts state ─────────────────────────────────────────
	let deletingNoteId = $state<string | null>(null);
	// ── Audit Log state ──────────────────────────────────────
	const queryLimitOptions: ComboboxOption[] = [
		{ value: '50', label: '50 queries' },
		{ value: '100', label: '100 queries' },
		{ value: '500', label: '500 queries' },
		{ value: '1000', label: '1000 queries' }
	];

	const databaseFilterOptions = $derived.by<ComboboxOption[]>(() => [
		{ value: '', label: 'All Databases' },
		...databases.map((db) => ({
			value: db.name,
			label: db.name
		}))
	]);

	// Helper functions
	// DataTable is typed over Record rows; our row types are interfaces, so
	// they need this cast on the way in and back inside the snippets.
	type Row = Record<string, unknown>;
	const asRows = <T,>(rows: T[]): Row[] => rows as unknown as Row[];

	function dayLabel(v: number): string {
		return new Date(v * 1000).toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short', timeZone: 'UTC' });
	}

	const tableColumns: DataColumn<Row>[] = [
		{ key: 'database_name', label: 'Database' },
		{ key: 'table_name', label: 'Table' },
		{ key: 'engine', label: 'Engine' },
		{ key: 'total_rows', label: 'Rows', align: 'right', format: (v) => Number(v ?? 0).toLocaleString() },
		{ key: 'total_bytes', label: 'Size', align: 'right', format: (v) => formatBytes(Number(v ?? 0)) },
		{ key: 'tags', label: 'Tags', sortable: false },
	];
	const queryColumns: DataColumn<Row>[] = [
		{ key: 'event_time', label: 'Time', mono: true, format: (v) => formatDate(v) },
		{ key: 'ch_user', label: 'User' },
		{ key: 'query_kind', label: 'Type' },
		{ key: 'query_text', label: 'Query', mono: true, truncate: true, sortable: false },
		{ key: 'duration_ms', label: 'Duration', align: 'right', format: (v) => `${v ?? 0} ms` },
		{ key: 'read_rows', label: 'Rows', align: 'right', format: (v) => Number(v ?? 0).toLocaleString() },
	];
	function truncate(s: string, max = 80): string {
		if (!s) return '';
		return s.length > max ? s.substring(0, max) + '...' : s;
	}

	function toDayKey(ts: string): string {
		const d = new Date(ts);
		if (Number.isNaN(d.getTime())) return '';
		const y = d.getUTCFullYear();
		const m = String(d.getUTCMonth() + 1).padStart(2, '0');
		const day = String(d.getUTCDate()).padStart(2, '0');
		return `${y}-${m}-${day}`;
	}

	function buildRecentDailySeries(dates: string[], days = 7): { x: number[]; y: number[] } {
		const now = new Date();
		const start = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()));
		start.setUTCDate(start.getUTCDate() - (days - 1));

		const keys: string[] = [];
		const x: number[] = [];
		const y: number[] = [];
		for (let i = 0; i < days; i++) {
			const day = new Date(start);
			day.setUTCDate(start.getUTCDate() + i);
			const key = `${day.getUTCFullYear()}-${String(day.getUTCMonth() + 1).padStart(2, '0')}-${String(day.getUTCDate()).padStart(2, '0')}`;
			keys.push(key);
			x.push(Math.floor(day.getTime() / 1000));
			y.push(0);
		}
		const idx = new Map(keys.map((k, i) => [k, i]));
		for (const ts of dates) {
			const key = toDayKey(ts);
			const pos = idx.get(key);
			if (pos !== undefined) y[pos] += 1;
		}
		return { x, y };
	}

	function syncStatusLabel(status: SyncState['status']): string {
		if (status === 'idle') return 'synced';
		return status;
	}

	const governanceTabIds: GovernanceTab[] = ['dashboard', 'tables', 'queries', 'access', 'incidents', 'policies', 'alerts', 'auditlog', 'settings'];


	function normalizeGovernanceTab(value: string | null | undefined): GovernanceTab {
		const raw = (value ?? '').trim().toLowerCase();
		if (raw === 'query-audit' || raw === 'queryaudit') return 'queries';
		if ((governanceTabIds as string[]).includes(raw)) return raw as GovernanceTab;
		return 'dashboard';
	}

	function syncGovernanceTabParam(tab: GovernanceTab) {
		if (typeof window === 'undefined') return
		if (!window.location.pathname.endsWith('/governance')) return
		setSection(tab)
	}

	type BadgeTone = 'neutral' | 'success' | 'warning' | 'danger' | 'info' | 'brand';

	/** Left rail color for an over-permission group; the card itself stays neutral. */
	/** Badge tone for any severity string used across governance. */
	function severityTone(severity: string | null | undefined): BadgeTone {
		if (severity === 'critical' || severity === 'error') return 'danger';
		if (severity === 'warn') return 'warning';
		if (severity === 'info') return 'info';
		return 'neutral';
	}

	function syncStatusTone(status: SyncState['status']): BadgeTone {
		if (status === 'idle') return 'success';
		if (status === 'running') return 'info';
		if (status === 'error') return 'danger';
		return 'neutral';
	}

	const activeTabLabel = $derived(governanceTabs.find((t) => t.id === activeTab)?.label ?? '');

	function switchTab(tab: GovernanceTab, syncUrl = true) {
		activeTab = tab;
		if (syncUrl) syncGovernanceTabParam(tab);
		// Load data for the new tab
		if (tab === 'dashboard') {
			loadDashboard();
		} else if (tab === 'tables') {
			loadTables();
		} else if (tab === 'queries') {
			loadQueries();
		}
		// access, incidents, policies, alerts, auditlog and settings load
		// inside their own section components.
	}

	async function loadGovernanceSettings() {
		try {
			govSettings = await fetchGovernanceSettings();
		} catch (err: any) {
			toastError('Failed to load governance settings: ' + err.message);
		}
	}

	async function persistGovernanceSettings(payload: { sync_enabled?: boolean; banner_dismissed?: boolean }) {
		govToggleSaving = true;
		try {
			govSettings = await updateGovernanceSettings(payload);
			if (payload.sync_enabled === true) toastSuccess('Governance sync enabled');
			if (payload.sync_enabled === false) toastSuccess('Governance sync disabled');
		} catch (err: any) {
			toastError('Failed to update governance settings: ' + err.message);
		} finally {
			govToggleSaving = false;
		}
	}

	async function dismissGovernanceUpgradeBanner() {
		await persistGovernanceSettings({ banner_dismissed: true });
	}

	async function loadDashboard() {
		loading = true;
		try {
			overview = await fetchOverview();
		} catch (err: any) {
			toastError('Failed to load dashboard: ' + err.message);
		} finally {
			loading = false;
		}
	}

	async function loadTables() {
		loading = true;
		try {
			const [dbsRes, tablesRes] = await Promise.all([
				fetchDatabases(),
				fetchTables()
			]);
			databases = dbsRes?.databases ?? [];
			tables = tablesRes?.tables ?? [];
		} catch (err: any) {
			toastError('Failed to load tables: ' + err.message);
		} finally {
			loading = false;
		}
	}

	async function loadQueries() {
		loading = true;
		try {
			const logRes = await fetchQueryLog({ user: queryUserFilter || undefined, limit: queryLimit });
			queryLog = logRes?.entries ?? [];
		} catch (err: any) {
			toastError('Failed to load query audit: ' + err.message);
		} finally {
			loading = false;
		}
	}

	async function handleSyncNow() {
		if (govSettings && !govSettings.sync_enabled) {
			toastError('Governance sync is disabled. Enable it in the Settings tab first.');
			switchTab('settings');
			return;
		}
		syncing = true;
		try {
			await triggerSync();
			toastSuccess('Sync started successfully');
			// Reload dashboard after a delay
			setTimeout(() => {
				loadDashboard();
			}, 2000);
		} catch (err: any) {
			toastError('Failed to start sync: ' + err.message);
		} finally {
			syncing = false;
		}
	}

	async function openTableDetails(table: GovTable) {
		selectedTable = table;
		selectedTableColumns = [];
		tableNotes = [];
		tableNoteDraft = '';
		tableDetailSheetOpen = true;
		tableDetailLoading = true;
		try {
			const detail = await fetchTableDetail(table.database_name, table.table_name);
			selectedTableColumns = detail.columns ?? [];
			const notesRes = await fetchTableNotes(table.database_name, table.table_name);
			tableNotes = notesRes?.notes ?? [];
		} catch (err: any) {
			toastError('Failed to load table details: ' + err.message);
		} finally {
			tableDetailLoading = false;
		}
	}

	function closeTableDetails() {
		tableDetailSheetOpen = false;
		selectedTable = null;
		selectedTableColumns = [];
		tableNotes = [];
		tableNoteDraft = '';
	}

	async function addTableNote() {
		if (!selectedTable) return;
		const comment = tableNoteDraft.trim();
		if (!comment) return;
		try {
			await apiCreateTableNote(selectedTable.database_name, selectedTable.table_name, comment);
			tableNoteDraft = '';
			const notesRes = await fetchTableNotes(selectedTable.database_name, selectedTable.table_name);
			tableNotes = notesRes?.notes ?? [];
			toastSuccess('Table note added');
		} catch (err: any) {
			toastError('Failed to add table note: ' + err.message);
		}
	}

	function deleteTableNote(noteId: string) {
		deletingNoteId = noteId;
	}

	async function confirmDeleteNote() {
		if (!deletingNoteId || !selectedTable) return;
		const noteId = deletingNoteId;
		deletingNoteId = null;
		try {
			await apiDeleteObjectNote(noteId);
			const notesRes = await fetchTableNotes(selectedTable.database_name, selectedTable.table_name);
			tableNotes = notesRes?.notes ?? [];
			toastSuccess('Note deleted');
		} catch (err: any) {
			toastError('Failed to delete note: ' + err.message);
		}
	}

	function openQueryDetails(entry: QueryLogEntry) {
		selectedQuery = entry;
		queryDetailSheetOpen = true;
	}

	function closeQueryDetails() {
		queryDetailSheetOpen = false;
		selectedQuery = null;
	}

	// ── Alerts admin ─────────────────────────────────────────
	// ── Audit Log ────────────────────────────────────────────
	// Computed values
	let filteredTables = $derived(
		tables.filter((t) => {
			const matchesDb = !selectedDatabase || t.database_name === selectedDatabase;
			const matchesSearch =
				!tableSearchQuery ||
				t.table_name.toLowerCase().includes(tableSearchQuery.toLowerCase()) ||
				t.database_name.toLowerCase().includes(tableSearchQuery.toLowerCase());
			return matchesDb && matchesSearch;
		})
	);

	let dashboardViolationTrend = $derived.by(() => {
		const dates = (overview?.recent_violations ?? []).map((v) => v.detected_at).filter(Boolean);
		return buildRecentDailySeries(dates, 7);
	});

	let dashboardSchemaTrend = $derived.by(() => {
		const dates = (overview?.recent_changes ?? []).map((c) => c.detected_at).filter(Boolean);
		return buildRecentDailySeries(dates, 7);
	});

	onMount(() => {
		const initialTab = normalizeGovernanceTab(
			typeof window === 'undefined' ? null : new URLSearchParams(window.location.search).get('section')
		);
		switchTab(initialTab, true);
		// Load settings in the background so the upgrade banner + sync-state
		// indicator can render regardless of which tab the user lands on.
		loadGovernanceSettings();
	});

	// The context panel changes ?section=; follow it.
	$effect(() => {
		const next = getSection()
		if (next === null) return
		const tab = normalizeGovernanceTab(next)
		if (tab !== activeTab) switchTab(tab, false)
	})
</script>

<!-- Small label/value block for detail sheets: a fact, not a KPI. -->
{#snippet fact(label: string, value: string | number)}
	<div class="min-w-0 rounded-md bg-surface-2 px-3 py-2">
		<p class="text-[11px] font-medium uppercase tracking-wider text-fg-3">{label}</p>
		<p class="mt-0.5 truncate text-[13px] font-medium text-fg">{value}</p>
	</div>
{/snippet}

<div class="flex h-full min-h-0 flex-col">
	{#if govSettings && !govSettings.sync_enabled && !govSettings.banner_dismissed}
		<div class="flex items-start gap-3 border-b border-edge-subtle bg-info-soft px-5 py-3 text-[13px] text-fg">
			<Info size={16} class="mt-0.5 shrink-0 text-info" />
			<div class="min-w-0 flex-1">
				<p class="font-medium">Governance background sync is now opt-in.</p>
				<p class="mt-0.5 text-fg-2">
					Your existing data is preserved, but the syncer is paused until you enable it explicitly.
					<button
						class="ml-1 font-medium text-info underline underline-offset-2 hover:text-fg"
						onclick={() => switchTab('settings')}
					>Review settings</button>
				</p>
			</div>
			<Button icon variant="ghost" size="sm" aria-label="Dismiss banner" disabled={govToggleSaving} onclick={dismissGovernanceUpgradeBanner}>
				<X size={14} />
			</Button>
		</div>
	{/if}

	<PageHeader title="Governance" subtitle={activeTabLabel}>
		{#snippet actions()}
			<Button size="sm" variant="outline" loading={syncing} onclick={handleSyncNow}>Sync now</Button>
		{/snippet}
	</PageHeader>

	{#if activeTab === 'access'}
		<AccessSection />
	{:else if activeTab === 'incidents'}
		<IncidentsSection />
	{:else if activeTab === 'policies'}
		<PoliciesSection />
	{:else if activeTab === 'alerts'}
		<AlertsSection />
	{:else if activeTab === 'auditlog'}
		<AuditLogSection />
	{:else if activeTab === 'settings'}
		<SettingsSection />
	{:else}
	<PageBody width="lg">
		{#if loading && !overview && !tables.length && !queryLog.length}
			<div class="flex items-center justify-center py-12">
				<Spinner size="lg" />
			</div>
		{:else}
			<!-- Dashboard -->
			{#if activeTab === 'dashboard'}
				{#if overview}
					<div class="space-y-5">
						<div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
							<Stat label="Violations" value={overview.violation_count} tone={overview.violation_count > 0 ? 'danger' : 'default'} />
							<Stat label="Open incidents" value={overview.incident_count || 0} tone={(overview.incident_count || 0) > 0 ? 'warning' : 'default'} />
							<Stat label="Policies" value={overview.policy_count} />
							<Stat label="Queries (24h)" value={overview.query_count_24h} />
						</div>

						<Panel variant="muted" padding="sm">
							<p class="text-[13px] tabular-nums text-fg-3">
								<span class="font-medium text-fg">{overview.database_count}</span> {overview.database_count === 1 ? 'database' : 'databases'}
								<span class="mx-1.5 text-fg-4">·</span>
								<span class="font-medium text-fg">{overview.table_count}</span> {overview.table_count === 1 ? 'table' : 'tables'}
								<span class="mx-1.5 text-fg-4">·</span>
								<span class="font-medium text-fg">{overview.column_count}</span> {overview.column_count === 1 ? 'column' : 'columns'}
								<span class="mx-1.5 text-fg-4">·</span>
								<span class="font-medium text-fg">{overview.user_count}</span> {overview.user_count === 1 ? 'user' : 'users'}
								<span class="mx-1.5 text-fg-4">·</span>
								<span class="font-medium text-fg">{overview.tagged_table_count}</span> tagged {overview.tagged_table_count === 1 ? 'table' : 'tables'}
							</p>
						</Panel>

						<div class="grid grid-cols-1 gap-3 lg:grid-cols-2">
							<Panel title="Violations, last 7 days">
								{#snippet actions()}
									<HelpTip text="Policy violations detected per day over the last 7 days." />
								{/snippet}
								<MiniTrendChart x={dashboardViolationTrend.x} y={dashboardViolationTrend.y} label="Violations" formatX={dayLabel} color="var(--danger)" fill="var(--danger-soft)" height={110} />
							</Panel>
							<Panel title="Schema changes, last 7 days">
								{#snippet actions()}
									<HelpTip text="Metadata and schema change events per day over the last 7 days." />
								{/snippet}
								<MiniTrendChart x={dashboardSchemaTrend.x} y={dashboardSchemaTrend.y} label="Schema changes" formatX={dayLabel} color="var(--success)" fill="var(--success-soft)" height={110} />
							</Panel>
						</div>

						<Panel title="Sync status" description="Last run of each governance sync worker.">
							<div class="divide-y divide-edge-subtle">
								{#each overview.sync_states ?? [] as syncState}
									<div class="flex items-center justify-between gap-3 py-2 text-[13px]">
										<span class="font-medium text-fg">{syncState.sync_type}</span>
										<div class="flex items-center gap-3">
											<span class="text-xs text-fg-3">
												{syncState.last_synced_at ? formatDate(syncState.last_synced_at) : 'Never'}
											</span>
											<Badge tone={syncStatusTone(syncState.status)}>{syncStatusLabel(syncState.status)}</Badge>
										</div>
									</div>
								{/each}
							</div>
						</Panel>

						<div class="grid grid-cols-1 gap-3 md:grid-cols-2">
							<Panel title="Recent schema changes">
								{#if overview.recent_changes && overview.recent_changes.length > 0}
									<div class="divide-y divide-edge-subtle">
										{#each overview.recent_changes as change}
											<div class="py-2 text-[13px]">
												<div class="flex items-center justify-between gap-3">
													<span class="truncate font-medium text-fg">{change.database_name}.{change.table_name}</span>
													<span class="shrink-0 text-xs text-fg-3">{formatDate(change.detected_at)}</span>
												</div>
												<p class="mt-0.5 text-xs text-fg-2">{change.change_type}</p>
											</div>
										{/each}
									</div>
								{:else}
									<EmptyState size="compact" title="No recent changes" />
								{/if}
							</Panel>

							<Panel title="Recent violations">
								{#if overview.recent_violations && overview.recent_violations.length > 0}
									<div class="divide-y divide-edge-subtle">
										{#each overview.recent_violations as violation}
											<div class="py-2 text-[13px]">
												<div class="flex items-center justify-between gap-3">
													<span class="truncate font-medium text-fg">{violation.policy_name}</span>
													<Badge tone={severityTone(violation.severity)}>{violation.severity}</Badge>
												</div>
												<p class="mt-0.5 text-xs text-fg-2">{truncate(violation.violation_detail, 60)}</p>
												<p class="mt-0.5 text-xs text-fg-3">{formatDate(violation.detected_at)}</p>
											</div>
										{/each}
									</div>
								{:else}
									<EmptyState size="compact" title="No recent violations" />
								{/if}
							</Panel>
						</div>
					</div>
				{/if}
			{/if}

			<!-- Tables -->
			{#if activeTab === 'tables'}
				<div class="space-y-5">
					<div class="flex flex-col gap-3 md:flex-row">
						<div class="flex-1">
							<Input type="search" placeholder="Search tables…" bind:value={tableSearchQuery} />
						</div>
						<div class="w-full md:w-64">
							<Combobox
								options={databaseFilterOptions}
								value={selectedDatabase}
								onChange={(v) => selectedDatabase = v}
								placeholder="All Databases"
							/>
						</div>
					</div>

					{#if filteredTables.length > 0}
						<DataTable columns={tableColumns} rows={asRows(filteredTables)} rowKey={(r) => `${r.database_name}.${r.table_name}`} sort={{ key: 'table_name', dir: 'asc' }}>
							{#snippet cell(row, col, value)}
								{#if col.key === 'tags'}
									{@const tags = (row as unknown as GovTable).tags}
									{#if tags && tags.length > 0}
										<div class="flex flex-wrap gap-1">
											{#each tags as tag}<Badge tone="brand">{tag}</Badge>{/each}
										</div>
									{:else}
										<span class="text-fg-4">-</span>
									{/if}
								{:else if col.key === 'table_name'}
									<span class="font-medium text-fg">{value}</span>
								{:else}
									{value}
								{/if}
							{/snippet}
							{#snippet actions(row)}
								<Button size="xs" variant="outline" onclick={() => openTableDetails(row as unknown as GovTable)}>
									<PanelRightOpen size={12} />
									View
								</Button>
							{/snippet}
						</DataTable>
					{:else}
						<EmptyState icon={Table2} title="No tables found" description="Nothing matches the current search and database filter." />
					{/if}
				</div>
			{/if}

			<!-- Query Audit -->
			{#if activeTab === 'queries'}
				<div class="space-y-5">
					{#if queryLog.length === 0 && !loading}
						<p class="flex items-center gap-2 text-[13px] text-fg-3">
							<Info size={14} class="shrink-0 text-fg-4" />
							<span>
								Harvesting may be paused. Check the mode under
								<button class="font-medium text-fg underline underline-offset-2 hover:text-accent" onclick={() => switchTab('settings')}>Settings</button>.
							</span>
						</p>
					{/if}

					<div class="flex flex-col gap-3 md:flex-row">
						<div class="flex-1">
							<Input placeholder="Filter by user…" bind:value={queryUserFilter} />
						</div>
						<div class="w-full md:w-48">
							<Combobox
								options={queryLimitOptions}
								value={String(queryLimit)}
								onChange={(v) => {
									queryLimit = Number(v) || 100;
									void loadQueries();
								}}
								placeholder="Query limit"
							/>
						</div>
						<Button size="md" onclick={() => loadQueries()}>Apply filters</Button>
					</div>

					{#if queryLog.length > 0}
						<DataTable columns={queryColumns} rows={asRows(queryLog)} sort={{ key: 'event_time', dir: 'desc' }}>
							{#snippet cell(row, col, value)}
								{#if col.key === 'query_kind'}
									<Badge tone={(row as unknown as QueryLogEntry).is_error ? 'danger' : 'success'}>{value}</Badge>
								{:else if col.key === 'ch_user'}
									<span class="font-medium text-fg">{value}</span>
								{:else}
									{value}
								{/if}
							{/snippet}
							{#snippet actions(row)}
								<Button size="xs" variant="outline" onclick={() => openQueryDetails(row as unknown as QueryLogEntry)}>
									<PanelRightOpen size={12} />
									View
								</Button>
							{/snippet}
						</DataTable>
					{:else}
						<EmptyState icon={Search} title="No query logs found" description="Nothing has been harvested for this filter yet." />
					{/if}
				</div>
			{/if}
		{/if}
	</PageBody>
	{/if}
</div>

	<Sheet
		open={tableDetailSheetOpen}
		title={selectedTable ? `Table Details · ${selectedTable.database_name}.${selectedTable.table_name}` : 'Table Details'}
		size="xl"
		onclose={closeTableDetails}
	>
		{#if selectedTable}
			<div class="space-y-5">
				<div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
					{@render fact('Engine', selectedTable.engine || '-')}
					{@render fact('Rows', selectedTable.total_rows.toLocaleString())}
					{@render fact('Size', formatBytes(selectedTable.total_bytes))}
					{@render fact('Partitions', selectedTable.partition_count)}
				</div>

				<Panel title="Columns" padding="none">
					{#snippet actions()}
						<span class="text-xs text-fg-3">{selectedTableColumns.length} total</span>
					{/snippet}
					{#if tableDetailLoading}
						<div class="flex justify-center py-10">
							<Spinner size="md" />
						</div>
					{:else if selectedTableColumns.length > 0}
						<div class="ds-table-wrap">
							<table class="ds-table">
								<thead>
									<tr class="ds-table-head-row">
										<th class="ds-table-th">Column</th>
										<th class="ds-table-th">Type</th>
										<th class="ds-table-th">Default</th>
										<th class="ds-table-th">Tags</th>
									</tr>
								</thead>
								<tbody>
									{#each selectedTableColumns as col}
										<tr class="ds-table-row-static">
											<td class="ds-td-strong">{col.column_name}</td>
											<td class="ds-td-mono">{col.column_type}</td>
											<td class="ds-td text-fg-3">{col.default_expression || '-'}</td>
											<td class="ds-td">
												{#if col.tags?.length}
													<div class="flex flex-wrap gap-1">
														{#each col.tags as tag}
															<Badge tone="brand">{tag}</Badge>
														{/each}
													</div>
												{:else}
													<span class="text-fg-4">-</span>
												{/if}
											</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{:else}
						<EmptyState size="compact" title="No column metadata available" />
					{/if}
				</Panel>

				<Panel title="Governance notes" description="Owners, SLA, sensitivity, remediation steps.">
					<div class="space-y-3">
						<div class="flex gap-2">
							<Textarea class="flex-1" rows={2} placeholder="Add a note for this table…" bind:value={tableNoteDraft} />
							<Button size="md" class="self-start" onclick={() => addTableNote()}>
								<Plus size={14} />
								Add
							</Button>
						</div>

						{#if tableNotes.length > 0}
							<div class="max-h-64 space-y-2 overflow-auto">
								{#each tableNotes as note}
									<div class="rounded-md bg-surface-2 p-3">
										<div class="flex items-start justify-between gap-3">
											<div class="min-w-0">
												<p class="whitespace-pre-wrap text-[13px] text-fg">{note.comment_text}</p>
												<p class="mt-1 text-xs text-fg-3">
													{note.created_by || 'unknown'} · {formatDate(note.created_at)}
												</p>
											</div>
											<Button icon variant="ghost" size="xs" aria-label="Delete note" title="Delete note" onclick={() => deleteTableNote(note.id)}>
												<Trash2 size={13} />
											</Button>
										</div>
									</div>
								{/each}
							</div>
						{:else}
							<p class="text-[13px] text-fg-3">No table notes yet.</p>
						{/if}
					</div>
				</Panel>
			</div>
		{/if}
	</Sheet>

	<Sheet
		open={queryDetailSheetOpen}
		title={selectedQuery ? `Query Details · ${selectedQuery.ch_user}` : 'Query Details'}
		size="xl"
		onclose={closeQueryDetails}
	>
		{#if selectedQuery}
			<div class="space-y-5">
				<div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
					{@render fact('Kind', selectedQuery.query_kind)}
					{@render fact('Duration', `${selectedQuery.duration_ms} ms`)}
					{@render fact('Read rows', selectedQuery.read_rows.toLocaleString())}
					{@render fact('Timestamp', formatDate(selectedQuery.event_time))}
				</div>

				<Panel title="SQL" padding="none">
					<pre class="overflow-x-auto border-t border-edge-subtle bg-canvas p-4 font-mono text-xs text-fg">{selectedQuery.query_text}</pre>
				</Panel>

				{#if selectedQuery.error_message}
					<div class="rounded-lg bg-danger-soft p-4">
						<p class="mb-1 text-[11px] font-medium uppercase tracking-wider text-danger">Error</p>
						<p class="text-[13px] text-fg">{selectedQuery.error_message}</p>
					</div>
				{/if}
			</div>
		{/if}
	</Sheet>

<ConfirmDialog
	open={deletingNoteId !== null}
	title="Delete note?"
	description="Are you sure you want to delete this note? This cannot be undone."
	confirmLabel="Delete"
	destructive
	onconfirm={confirmDeleteNote}
	oncancel={() => deletingNoteId = null}
/>

