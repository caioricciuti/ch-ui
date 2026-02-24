<script lang="ts">
	import { onMount } from 'svelte';
	import {
		RefreshCw,
		Database,
		Table2,
		Columns3,
		Users,
		Search,
		GitBranch,
		Shield,
		AlertTriangle,
		Plus,
		Edit,
		Trash2,
		PanelRightOpen,
		ChevronRight,
		ChevronDown,
		MessageSquare,
		Siren,
		Bell
	} from 'lucide-svelte';
	import Spinner from '../lib/components/common/Spinner.svelte';
	import Combobox from '../lib/components/common/Combobox.svelte';
	import type { ComboboxOption } from '../lib/components/common/Combobox.svelte';
	import ConfirmDialog from '../lib/components/common/ConfirmDialog.svelte';
	import Sheet from '../lib/components/common/Sheet.svelte';
	import HelpTip from '../lib/components/common/HelpTip.svelte';
	import MiniTrendChart from '../lib/components/common/MiniTrendChart.svelte';
	import { success as toastSuccess, error as toastError } from '../lib/stores/toast.svelte';
	import {
		fetchOverview,
		fetchDatabases,
		fetchTables,
		fetchTableDetail,
		fetchQueryLog,
		fetchTopQueries,
		fetchLineageGraph,
		fetchAccessUsers,
		fetchAccessRoles,
		fetchAccessMatrix,
		fetchOverPermissions,
		fetchPolicies,
		createPolicy as apiCreatePolicy,
		updatePolicy as apiUpdatePolicy,
		deletePolicy as apiDeletePolicy,
		fetchViolations,
		promoteViolationToIncident,
		fetchIncidents,
		createIncident as apiCreateIncident,
		updateIncident as apiUpdateIncident,
		getIncident as apiGetIncident,
		fetchIncidentComments,
		createIncidentComment as apiCreateIncidentComment,
		fetchTableNotes,
		createTableNote as apiCreateTableNote,
		deleteObjectNote as apiDeleteObjectNote,
		triggerSync
	} from '../lib/api/governance';
	import { apiGet } from '../lib/api/client';
	import type { AlertChannel, AlertChannelType, AlertEvent, AlertRule } from '../lib/types/alerts';
	import {
		adminListAlertChannels,
		adminCreateAlertChannel,
		adminUpdateAlertChannel,
		adminDeleteAlertChannel,
		adminTestAlertChannel,
		adminListAlertRules,
		adminCreateAlertRule,
		adminUpdateAlertRule,
		adminDeleteAlertRule,
		adminListAlertEvents,
	} from '../lib/api/alerts';
	import type { AlertRuleRoutePayload } from '../lib/api/alerts';
	import type { AuditLog } from '../lib/types/api';
	import type {
		GovernanceOverview,
		GovDatabase,
		GovTable,
		GovColumn,
		QueryLogEntry,
		TopQuery,
		LineageEdge,
		ChUser,
		ChRole,
		AccessMatrixEntry,
		OverPermission,
		Policy,
		PolicyViolation,
		GovernanceIncident,
		GovernanceIncidentComment,
		GovernanceObjectComment,
		SyncState
	} from '../lib/types/governance';

	// State
	type GovernanceTab = 'dashboard' | 'tables' | 'queries' | 'lineage' | 'access' | 'incidents' | 'policies' | 'querylog' | 'alerts' | 'auditlog';
	type OverPermissionGroup = {
		userName: string;
		alerts: OverPermission[];
		total: number;
		databases: number;
		critical: number;
		warn: number;
		info: number;
		topSeverity: 'critical' | 'warn' | 'info';
	};
	const governanceTabs: Array<{ id: GovernanceTab; label: string }> = [
		{ id: 'dashboard', label: 'Dashboard' },
		{ id: 'tables', label: 'Tables' },
		{ id: 'queries', label: 'Query Audit' },
		{ id: 'lineage', label: 'Lineage' },
		{ id: 'access', label: 'Access' },
		{ id: 'incidents', label: 'Incidents' },
		{ id: 'policies', label: 'Policies' },
		{ id: 'querylog', label: 'Query Log' },
		{ id: 'alerts', label: 'Alerts' },
		{ id: 'auditlog', label: 'Audit Log' },
	];

	let activeTab = $state<GovernanceTab>('dashboard');
	let loading = $state<boolean>(false);
	let syncing = $state<boolean>(false);

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
	let topQueries = $state<TopQuery[]>([]);
	let queryUserFilter = $state<string>('');
	let queryLimit = $state<number>(100);
	let queryDetailSheetOpen = $state<boolean>(false);
	let selectedQuery = $state<QueryLogEntry | null>(null);

	// Lineage data
	let lineageEdges = $state<LineageEdge[]>([]);

	// Access data
	let users = $state<ChUser[]>([]);
	let roles = $state<ChRole[]>([]);
	let accessMatrix = $state<AccessMatrixEntry[]>([]);
	let overPermissions = $state<OverPermission[]>([]);
	let accessUserFilter = $state<string>('');
	let accessDatabaseFilter = $state<string>('');
	let accessDetailSheetOpen = $state<boolean>(false);
	let selectedOverPermission = $state<OverPermission | null>(null);
	let selectedOverPermissionGroup = $state<OverPermissionGroup | null>(null);
	let expandedOverPermissionUsers = $state<Record<string, boolean>>({});

	// Policies data
	let policies = $state<Policy[]>([]);
	let violations = $state<PolicyViolation[]>([]);
	let showPolicyForm = $state<boolean>(false);
	let editingPolicy = $state<Policy | null>(null);
	let confirmPolicyDeleteOpen = $state<boolean>(false);
	let confirmPolicyDeleteLoading = $state<boolean>(false);
	let pendingPolicyDelete = $state<Policy | null>(null);
	let incidents = $state<GovernanceIncident[]>([]);
	let incidentStatusFilter = $state<string>('');
	let incidentSeverityFilter = $state<string>('');
	let incidentDetailSheetOpen = $state<boolean>(false);
	let selectedIncident = $state<GovernanceIncident | null>(null);
	let incidentComments = $state<GovernanceIncidentComment[]>([]);
	let incidentCommentDraft = $state<string>('');
	let incidentCreateSheetOpen = $state<boolean>(false);
	let incidentForm = $state({
		title: '',
		severity: 'warn',
		status: 'open',
		assignee: '',
		details: ''
	});
	let policyForm = $state<{
		name: string;
		description: string;
		object_type: Policy['object_type'];
		object_database: string;
		object_table: string;
		object_column: string;
		required_role: string;
		severity: string;
		enforcement_mode: Policy['enforcement_mode'];
		enabled: boolean;
	}>({
		name: '',
		description: '',
		object_type: 'table',
		object_database: '',
		object_table: '',
		object_column: '',
		required_role: '',
		severity: 'warn',
		enforcement_mode: 'warn',
		enabled: true
	});

	// ── ClickHouse Query Log state ───────────────────────────
	let queryLogLoading = $state(false);
	let queryLogData = $state<any[]>([]);
	let queryLogMeta = $state<any[]>([]);
	let qlTimeRange = $state('1h');
	let qlSearch = $state('');
	let qlQueryKind = $state('');
	let qlStatus = $state('');
	let qlLimit = $state(100);
	let qlOffset = $state(0);
	let expandedRow = $state<number | null>(null);

	// ── Alerts state ─────────────────────────────────────────
	let alertsLoading = $state(false);
	let alertChannels = $state<AlertChannel[]>([]);
	let alertRules = $state<AlertRule[]>([]);
	let alertEvents = $state<AlertEvent[]>([]);
	let alertEventLimit = $state(50);
	let alertTestRecipients = $state('');
	let channelSheetOpen = $state(false);
	let ruleSheetOpen = $state(false);
	type RuleRouteDraft = {
		channel_id: string;
		recipients: string;
		is_active: boolean;
		delivery_mode: 'immediate' | 'digest';
		digest_window_minutes: number;
		escalation_channel_id: string;
		escalation_recipients: string;
		escalation_after_failures: number;
	};
	let ruleRoutesDraft = $state<RuleRouteDraft[]>([{
		channel_id: '',
		recipients: '',
		is_active: true,
		delivery_mode: 'immediate',
		digest_window_minutes: 15,
		escalation_channel_id: '',
		escalation_recipients: '',
		escalation_after_failures: 0,
	}]);
	let channelForm = $state({
		name: '',
		channel_type: 'smtp' as AlertChannelType,
		is_active: true,
		smtp_host: '',
		smtp_port: 587,
		smtp_username: '',
		smtp_password: '',
		smtp_from_email: '',
		smtp_from_name: '',
		smtp_use_tls: true,
		smtp_starttls: false,
		api_key: '',
		api_from_email: '',
		api_from_name: '',
		api_base_url: '',
	});
	let ruleForm = $state({
		name: '',
		event_type: 'policy.violation',
		severity_min: 'warn',
		enabled: true,
		cooldown_seconds: 300,
		max_attempts: 3,
		subject_template: '',
		body_template: '',
	});

	// ── Audit Log state ──────────────────────────────────────
	let auditLogs = $state<AuditLog[]>([]);
	let auditLoading = $state(false);
	let auditLimit = $state(100);
	let auditTimeRange = $state('');
	let auditAction = $state('');
	let auditUsername = $state('');
	let auditSearch = $state('');

	const queryLimitOptions: ComboboxOption[] = [
		{ value: '50', label: '50 queries' },
		{ value: '100', label: '100 queries' },
		{ value: '500', label: '500 queries' },
		{ value: '1000', label: '1000 queries' }
	];

	const policyObjectTypeOptions: ComboboxOption[] = [
		{ value: 'database', label: 'Database' },
		{ value: 'table', label: 'Table' },
		{ value: 'column', label: 'Column' }
	];

	const policySeverityOptions: ComboboxOption[] = [
		{ value: 'info', label: 'Info' },
		{ value: 'warn', label: 'Warning' },
		{ value: 'critical', label: 'Critical' }
	];

	const policyEnforcementModeOptions: ComboboxOption[] = [
		{ value: 'warn', label: 'Warn (allow)' },
		{ value: 'block', label: 'Block (deny)' }
	];

	const incidentSeverityOptions: ComboboxOption[] = [
		{ value: '', label: 'All Severities' },
		{ value: 'info', label: 'info' },
		{ value: 'warn', label: 'warn' },
		{ value: 'error', label: 'error' },
		{ value: 'critical', label: 'critical' }
	];

	const incidentStatusOptions: ComboboxOption[] = [
		{ value: '', label: 'All Statuses' },
		{ value: 'open', label: 'open' },
		{ value: 'triaged', label: 'triaged' },
		{ value: 'in_progress', label: 'in_progress' },
		{ value: 'resolved', label: 'resolved' },
		{ value: 'dismissed', label: 'dismissed' }
	];

	const databaseFilterOptions = $derived.by<ComboboxOption[]>(() => [
		{ value: '', label: 'All Databases' },
		...databases.map((db) => ({
			value: db.name,
			label: db.name
		}))
	]);

	const qlTimeRangeOptions: ComboboxOption[] = [
		{ value: '5m', label: '5 min' },
		{ value: '15m', label: '15 min' },
		{ value: '30m', label: '30 min' },
		{ value: '1h', label: '1 hour' },
		{ value: '6h', label: '6 hours' },
		{ value: '12h', label: '12 hours' },
		{ value: '24h', label: '24 hours' },
		{ value: '3d', label: '3 days' },
		{ value: '7d', label: '7 days' },
	];

	const alertChannelTypeOptions: ComboboxOption[] = [
		{ value: 'smtp', label: 'SMTP' },
		{ value: 'resend', label: 'Resend' },
		{ value: 'brevo', label: 'Brevo' },
	];

	const alertEventTypeOptions: ComboboxOption[] = [
		{ value: 'policy.violation', label: 'Policy Violation' },
		{ value: 'schedule.failed', label: 'Schedule Failed' },
		{ value: 'schedule.slow', label: 'Schedule Slow' },
		{ value: '*', label: 'All Events' },
	];

	const alertSeverityOptions: ComboboxOption[] = [
		{ value: 'info', label: 'Info' },
		{ value: 'warn', label: 'Warning' },
		{ value: 'error', label: 'Error' },
		{ value: 'critical', label: 'Critical' },
	];

	const routeDeliveryModeOptions: ComboboxOption[] = [
		{ value: 'immediate', label: 'Immediate' },
		{ value: 'digest', label: 'Digest' },
	];

	const auditLimitOptions: ComboboxOption[] = [
		{ value: '50', label: '50 entries' },
		{ value: '100', label: '100 entries' },
		{ value: '500', label: '500 entries' },
	];
	const auditTimeRangeOptions: ComboboxOption[] = [
		{ value: '', label: 'All time', keywords: 'all' },
		{ value: '15m', label: '15 min' },
		{ value: '1h', label: '1 hour' },
		{ value: '6h', label: '6 hours' },
		{ value: '24h', label: '24 hours' },
		{ value: '7d', label: '7 days' },
		{ value: '30d', label: '30 days' },
	];
	const auditActionOptions = $derived.by((): ComboboxOption[] => {
		const values = new Set<string>();
		if (auditAction.trim()) values.add(auditAction);
		for (const log of auditLogs) {
			if (log.action?.trim()) values.add(log.action);
		}
		const sorted = Array.from(values).sort((a, b) => a.localeCompare(b));
		return [{ value: '', label: 'All actions', keywords: 'all' }, ...sorted.map((value) => ({ value, label: value }))];
	});
	const auditUsernameOptions = $derived.by((): ComboboxOption[] => {
		const values = new Set<string>();
		if (auditUsername.trim()) values.add(auditUsername);
		for (const log of auditLogs) {
			if (log.username?.trim()) values.add(log.username);
		}
		const sorted = Array.from(values).sort((a, b) => a.localeCompare(b));
		return [{ value: '', label: 'All users', keywords: 'all' }, ...sorted.map((value) => ({ value, label: value }))];
	});

	// Helper functions
	function formatBytes(bytes: number): string {
		if (bytes === 0) return '0 B';
		const k = 1024;
		const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
		const i = Math.floor(Math.log(bytes) / Math.log(k));
		return Math.round(bytes / Math.pow(k, i) * 100) / 100 + ' ' + sizes[i];
	}

	function formatTime(ts: string): string {
		if (!ts) return '-';
		const date = new Date(ts);
		return date.toLocaleString();
	}

	function truncate(s: string, max = 80): string {
		if (!s) return '';
		return s.length > max ? s.substring(0, max) + '...' : s;
	}

	function safeLower(v: string | null | undefined): string {
		return (v ?? '').toLowerCase();
	}

	function formatDefaultRoles(raw: string | null | undefined): string {
		if (!raw) return '-';
		if (raw === 'ALL') return 'ALL';
		const trimmed = raw.trim();
		if (!trimmed) return '-';

		try {
			if (trimmed.startsWith('[') && trimmed.endsWith(']')) {
				const parsed = JSON.parse(trimmed);
				if (Array.isArray(parsed)) {
					return parsed.filter(Boolean).join(', ') || '-';
				}
			}
		} catch {
			// fall through
		}

		return trimmed;
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
			x.push(i + 1);
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

	function overPermissionSeverity(op: OverPermission): 'critical' | 'warn' | 'info' {
		if (!op.last_query_time) return 'critical';
		const days = op.days_since_query ?? 0;
		if (days >= 90) return 'critical';
		if (days >= 30) return 'warn';
		return 'info';
	}

	const governanceTabIds: GovernanceTab[] = ['dashboard', 'tables', 'queries', 'lineage', 'access', 'incidents', 'policies', 'querylog', 'alerts', 'auditlog'];

	function normalizeGovernanceTab(value: string | null | undefined): GovernanceTab {
		const raw = (value ?? '').trim().toLowerCase();
		if (raw === 'query-audit' || raw === 'queryaudit') return 'queries';
		if ((governanceTabIds as string[]).includes(raw)) return raw as GovernanceTab;
		return 'dashboard';
	}

	function syncGovernanceTabParam(tab: GovernanceTab) {
		if (typeof window === 'undefined') return;
		const url = new URL(window.location.href);
		if (url.searchParams.get('tab') === tab) return;
		url.searchParams.set('tab', tab);
		history.replaceState(null, '', `${url.pathname}?${url.searchParams.toString()}`);
	}

	function overPermissionSeverityPriority(severity: 'critical' | 'warn' | 'info'): number {
		if (severity === 'critical') return 3;
		if (severity === 'warn') return 2;
		return 1;
	}

	function overPermissionPanelTone(severity: 'critical' | 'warn' | 'info'): string {
		if (severity === 'critical') return 'bg-red-50 border-red-200 dark:bg-red-900/20 dark:border-red-800';
		if (severity === 'warn') return 'bg-yellow-50 border-yellow-200 dark:bg-yellow-900/20 dark:border-yellow-800';
		return 'bg-blue-50 border-blue-200 dark:bg-blue-900/20 dark:border-blue-800';
	}

	function overPermissionBadgeTone(severity: 'critical' | 'warn' | 'info'): string {
		if (severity === 'critical') return 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200';
		if (severity === 'warn') return 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200';
		return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200';
	}

	function toggleOverPermissionGroup(userName: string) {
		const current = expandedOverPermissionUsers[userName] ?? false;
		expandedOverPermissionUsers = { ...expandedOverPermissionUsers, [userName]: !current };
	}

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
		} else if (tab === 'lineage') {
			loadLineage();
		} else if (tab === 'access') {
			loadAccess();
		} else if (tab === 'incidents') {
			loadIncidents();
		} else if (tab === 'policies') {
			loadPolicies();
		} else if (tab === 'querylog') {
			loadQueryLog();
		} else if (tab === 'alerts') {
			loadAlertsAdmin();
		} else if (tab === 'auditlog') {
			loadAuditLogs();
		}
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
			const [logRes, topRes] = await Promise.all([
				fetchQueryLog({ user: queryUserFilter || undefined, limit: queryLimit }),
				fetchTopQueries(10)
			]);
			queryLog = logRes?.entries ?? [];
			topQueries = topRes?.queries ?? [];
		} catch (err: any) {
			toastError('Failed to load query audit: ' + err.message);
		} finally {
			loading = false;
		}
	}

	async function loadLineage() {
		loading = true;
		try {
			const res = await fetchLineageGraph();
			lineageEdges = res?.edges ?? [];
		} catch (err: any) {
			toastError('Failed to load lineage: ' + err.message);
		} finally {
			loading = false;
		}
	}

	async function loadAccess() {
		loading = true;
		try {
			const [usersRes, rolesRes, matrixRes, overPermsRes] = await Promise.all([
				fetchAccessUsers(),
				fetchAccessRoles(),
				fetchAccessMatrix(),
				fetchOverPermissions()
			]);
			users = usersRes?.users ?? [];
			roles = rolesRes?.roles ?? [];
			accessMatrix = matrixRes?.matrix ?? [];
			overPermissions = overPermsRes?.over_permissions ?? [];
		} catch (err: any) {
			toastError('Failed to load access data: ' + err.message);
		} finally {
			loading = false;
		}
	}

	async function loadPolicies() {
		loading = true;
		try {
			const [policiesRes, violationsRes] = await Promise.all([
				fetchPolicies(),
				fetchViolations()
			]);
			policies = policiesRes?.policies ?? [];
			violations = violationsRes?.violations ?? [];
		} catch (err: any) {
			toastError('Failed to load policies: ' + err.message);
		} finally {
			loading = false;
		}
	}

	async function loadIncidents() {
		loading = true;
		try {
			const res = await fetchIncidents({
				status: incidentStatusFilter || undefined,
				severity: incidentSeverityFilter || undefined,
				limit: 200
			});
			incidents = res?.incidents ?? [];
		} catch (err: any) {
			toastError('Failed to load incidents: ' + err.message);
		} finally {
			loading = false;
		}
	}

	async function handleSyncNow() {
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

	async function deleteTableNote(noteId: string) {
		if (!confirm('Delete this note?')) return;
		if (!selectedTable) return;
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

	function openAccessDetails(alert: OverPermission) {
		selectedOverPermissionGroup = null;
		selectedOverPermission = alert;
		accessDetailSheetOpen = true;
	}

	function openAccessGroupDetails(group: OverPermissionGroup) {
		selectedOverPermission = null;
		selectedOverPermissionGroup = group;
		accessDetailSheetOpen = true;
	}

	function closeAccessDetails() {
		accessDetailSheetOpen = false;
		selectedOverPermission = null;
		selectedOverPermissionGroup = null;
	}

	function openIncidentDetails(incident: GovernanceIncident) {
		selectedIncident = incident;
		incidentComments = [];
		incidentCommentDraft = '';
		incidentDetailSheetOpen = true;
		void loadIncidentDetail(incident.id);
	}

	async function loadIncidentDetail(incidentID: string) {
		try {
			const [incidentRes, commentsRes] = await Promise.all([
				apiGetIncident(incidentID),
				fetchIncidentComments(incidentID)
			]);
			selectedIncident = incidentRes?.incident ?? selectedIncident;
			incidentComments = commentsRes?.comments ?? [];
		} catch (err: any) {
			toastError('Failed to load incident detail: ' + err.message);
		}
	}

	function closeIncidentDetails() {
		incidentDetailSheetOpen = false;
		selectedIncident = null;
		incidentComments = [];
		incidentCommentDraft = '';
	}

	async function createIncidentFromViolation(violation: PolicyViolation) {
		try {
			const res = await promoteViolationToIncident(violation.id);
			toastSuccess(res?.created ? 'Incident created from violation' : 'Existing incident updated');
			await loadIncidents();
		} catch (err: any) {
			toastError('Failed to promote violation: ' + err.message);
		}
	}

	async function createManualIncident() {
		const title = incidentForm.title.trim();
		if (!title) {
			toastError('Incident title is required');
			return;
		}
		try {
			await apiCreateIncident({
				title,
				severity: incidentForm.severity,
				status: incidentForm.status,
				assignee: incidentForm.assignee.trim() || undefined,
				details: incidentForm.details.trim() || undefined,
			});
			incidentCreateSheetOpen = false;
			incidentForm = {
				title: '',
				severity: 'warn',
				status: 'open',
				assignee: '',
				details: ''
			};
			toastSuccess('Incident created');
			await loadIncidents();
		} catch (err: any) {
			toastError('Failed to create incident: ' + err.message);
		}
	}

	async function saveIncidentUpdates() {
		if (!selectedIncident) return;
		try {
			await apiUpdateIncident(selectedIncident.id, {
				title: selectedIncident.title,
				severity: selectedIncident.severity,
				status: selectedIncident.status,
				assignee: selectedIncident.assignee ?? '',
				details: selectedIncident.details ?? '',
				resolution_note: selectedIncident.resolution_note ?? '',
			});
			toastSuccess('Incident updated');
			await loadIncidentDetail(selectedIncident.id);
			await loadIncidents();
		} catch (err: any) {
			toastError('Failed to update incident: ' + err.message);
		}
	}

	async function addIncidentComment() {
		if (!selectedIncident) return;
		const comment = incidentCommentDraft.trim();
		if (!comment) return;
		try {
			await apiCreateIncidentComment(selectedIncident.id, comment);
			incidentCommentDraft = '';
			await loadIncidentDetail(selectedIncident.id);
		} catch (err: any) {
			toastError('Failed to add incident comment: ' + err.message);
		}
	}

	function openPolicyForm(policy?: Policy) {
		if (policy) {
			editingPolicy = policy;
			policyForm = {
				name: policy.name,
				description: policy.description ?? '',
				object_type: policy.object_type,
				object_database: policy.object_database ?? '',
				object_table: policy.object_table ?? '',
				object_column: policy.object_column ?? '',
				required_role: policy.required_role,
				severity: policy.severity,
				enforcement_mode: policy.enforcement_mode ?? 'warn',
				enabled: policy.enabled
			};
		} else {
			editingPolicy = null;
			policyForm = {
				name: '',
				description: '',
				object_type: 'table',
				object_database: '',
				object_table: '',
				object_column: '',
				required_role: '',
				severity: 'warn',
				enforcement_mode: 'warn',
				enabled: true
			};
		}
		showPolicyForm = true;
	}

	function closePolicyForm() {
		showPolicyForm = false;
		editingPolicy = null;
	}

	async function handlePolicySubmit() {
		loading = true;
		try {
			if (editingPolicy) {
				await apiUpdatePolicy(editingPolicy.id, policyForm);
				toastSuccess('Policy updated successfully');
			} else {
				await apiCreatePolicy(policyForm);
				toastSuccess('Policy created successfully');
			}
			closePolicyForm();
			await loadPolicies();
		} catch (err: any) {
			toastError('Failed to save policy: ' + err.message);
		} finally {
			loading = false;
		}
	}

	function requestDeletePolicy(policy: Policy) {
		pendingPolicyDelete = policy;
		confirmPolicyDeleteOpen = true;
	}

	function cancelDeletePolicy() {
		confirmPolicyDeleteOpen = false;
		pendingPolicyDelete = null;
	}

	async function confirmDeletePolicy() {
		if (!pendingPolicyDelete) return;
		confirmPolicyDeleteLoading = true;
		try {
			await apiDeletePolicy(pendingPolicyDelete.id);
			toastSuccess('Policy deleted successfully');
			cancelDeletePolicy();
			await loadPolicies();
		} catch (err: any) {
			toastError('Failed to delete policy: ' + err.message);
		} finally {
			confirmPolicyDeleteLoading = false;
		}
	}

	// ── ClickHouse Query Log ─────────────────────────────────
	async function loadQueryLog() {
		queryLogLoading = true;
		try {
			const params = new URLSearchParams();
			if (qlTimeRange) params.set('timeRange', qlTimeRange);
			if (qlSearch.trim()) params.set('search', qlSearch.trim());
			if (qlQueryKind) params.set('queryKind', qlQueryKind);
			if (qlStatus) params.set('status', qlStatus);
			params.set('limit', String(qlLimit));
			params.set('offset', String(qlOffset));
			const res = await apiGet<{ data: any[]; meta: any[] }>(`/api/governance/clickhouse-query-log?${params}`);
			queryLogData = res.data ?? [];
			queryLogMeta = res.meta ?? [];
		} catch (e: any) {
			toastError(e.message);
		} finally {
			queryLogLoading = false;
		}
	}

	// ── Alerts admin ─────────────────────────────────────────
	async function loadAlertsAdmin() {
		alertsLoading = true;
		try {
			const [channels, rules, events] = await Promise.all([
				adminListAlertChannels(),
				adminListAlertRules(),
				adminListAlertEvents({ limit: alertEventLimit }),
			]);
			alertChannels = channels;
			alertRules = rules;
			alertEvents = events;
		} catch (e: any) {
			toastError(e.message);
		} finally {
			alertsLoading = false;
		}
	}

	async function createAlertChannelRecord() {
		const config: Record<string, unknown> = {};
		if (channelForm.channel_type === 'smtp') {
			config.smtp_host = channelForm.smtp_host;
			config.smtp_port = channelForm.smtp_port;
			config.smtp_username = channelForm.smtp_username;
			config.smtp_password = channelForm.smtp_password;
			config.from_email = channelForm.smtp_from_email;
			config.from_name = channelForm.smtp_from_name;
			config.use_tls = channelForm.smtp_use_tls;
			config.starttls = channelForm.smtp_starttls;
		} else {
			config.api_key = channelForm.api_key;
			config.from_email = channelForm.api_from_email;
			config.from_name = channelForm.api_from_name;
			if (channelForm.api_base_url) config.base_url = channelForm.api_base_url;
		}
		try {
			await adminCreateAlertChannel({
				name: channelForm.name,
				channel_type: channelForm.channel_type,
				is_active: channelForm.is_active,
				config,
			});
			toastSuccess('Alert channel created');
			channelSheetOpen = false;
			channelForm = { ...channelForm, name: '', smtp_host: '', smtp_port: 587, smtp_username: '', smtp_password: '', smtp_from_email: '', smtp_from_name: '', smtp_use_tls: true, smtp_starttls: false, api_key: '', api_from_email: '', api_from_name: '', api_base_url: '' };
			await loadAlertsAdmin();
		} catch (e: any) {
			toastError(e.message);
		}
	}

	async function toggleAlertChannel(channel: AlertChannel, isActive: boolean) {
		try {
			await adminUpdateAlertChannel(channel.id, { is_active: isActive });
			toastSuccess(`Channel "${channel.name}" ${isActive ? 'activated' : 'deactivated'}`);
			await loadAlertsAdmin();
		} catch (e: any) {
			toastError(e.message);
		}
	}

	async function deleteAlertChannelRecord(channel: AlertChannel) {
		if (!confirm(`Delete channel "${channel.name}"?`)) return;
		try {
			await adminDeleteAlertChannel(channel.id);
			toastSuccess('Channel deleted');
			await loadAlertsAdmin();
		} catch (e: any) {
			toastError(e.message);
		}
	}

	async function testAlertChannelRecord(channel: AlertChannel) {
		const recipients = alertTestRecipients.split(',').map((r) => r.trim()).filter(Boolean);
		if (recipients.length === 0) {
			toastError('Enter at least one test recipient');
			return;
		}
		try {
			await adminTestAlertChannel(channel.id, { recipients });
			toastSuccess('Test alert sent');
		} catch (e: any) {
			toastError(e.message);
		}
	}

	async function createAlertRuleRecord() {
		const routes: AlertRuleRoutePayload[] = ruleRoutesDraft.map((r) => ({
			channel_id: r.channel_id,
			recipients: r.recipients.split(',').map((s) => s.trim()).filter(Boolean),
			is_active: r.is_active,
			delivery_mode: r.delivery_mode,
			digest_window_minutes: r.delivery_mode === 'digest' ? r.digest_window_minutes : undefined,
			escalation_channel_id: r.escalation_channel_id || undefined,
			escalation_recipients: r.escalation_recipients ? r.escalation_recipients.split(',').map((s) => s.trim()).filter(Boolean) : undefined,
			escalation_after_failures: r.escalation_after_failures || undefined,
		}));
		try {
			await adminCreateAlertRule({
				name: ruleForm.name,
				event_type: ruleForm.event_type,
				severity_min: ruleForm.severity_min,
				enabled: ruleForm.enabled,
				cooldown_seconds: ruleForm.cooldown_seconds,
				max_attempts: ruleForm.max_attempts,
				subject_template: ruleForm.subject_template || undefined,
				body_template: ruleForm.body_template || undefined,
				routes,
			});
			toastSuccess('Alert rule created');
			ruleSheetOpen = false;
			ruleForm = { ...ruleForm, name: '' };
			ruleRoutesDraft = [{ channel_id: '', recipients: '', is_active: true, delivery_mode: 'immediate', digest_window_minutes: 15, escalation_channel_id: '', escalation_recipients: '', escalation_after_failures: 0 }];
			await loadAlertsAdmin();
		} catch (e: any) {
			toastError(e.message);
		}
	}

	async function toggleAlertRule(rule: AlertRule, enabled: boolean) {
		try {
			await adminUpdateAlertRule(rule.id, { enabled });
			toastSuccess(`Rule "${rule.name}" ${enabled ? 'enabled' : 'disabled'}`);
			await loadAlertsAdmin();
		} catch (e: any) {
			toastError(e.message);
		}
	}

	async function deleteAlertRuleRecord(rule: AlertRule) {
		if (!confirm(`Delete rule "${rule.name}"?`)) return;
		try {
			await adminDeleteAlertRule(rule.id);
			toastSuccess('Rule deleted');
			await loadAlertsAdmin();
		} catch (e: any) {
			toastError(e.message);
		}
	}

	function alertChannelOptions(): ComboboxOption[] {
		return alertChannels.map((ch) => ({ value: ch.id, label: `${ch.name} (${ch.channel_type})` }));
	}

	function addRuleRouteDraft() {
		ruleRoutesDraft = [...ruleRoutesDraft, { channel_id: '', recipients: '', is_active: true, delivery_mode: 'immediate', digest_window_minutes: 15, escalation_channel_id: '', escalation_recipients: '', escalation_after_failures: 0 }];
	}

	function removeRuleRouteDraft(idx: number) {
		ruleRoutesDraft = ruleRoutesDraft.filter((_, i) => i !== idx);
	}

	function updateRuleRouteDraft(idx: number, patch: Partial<RuleRouteDraft>) {
		ruleRoutesDraft = ruleRoutesDraft.map((r, i) => (i === idx ? { ...r, ...patch } : r));
	}

	// ── Audit Log ────────────────────────────────────────────
	async function loadAuditLogs() {
		auditLoading = true;
		try {
			const params = new URLSearchParams();
			params.set('limit', String(auditLimit));
			if (auditTimeRange) params.set('timeRange', auditTimeRange);
			if (auditAction) params.set('action', auditAction);
			if (auditUsername) params.set('username', auditUsername);
			if (auditSearch.trim()) params.set('search', auditSearch.trim());
			const res = await apiGet<AuditLog[]>(`/api/governance/audit-logs?${params}`);
			auditLogs = res ?? [];
		} catch (e: any) {
			toastError(e.message);
		} finally {
			auditLoading = false;
		}
	}

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

	let filteredAccessMatrix = $derived(
		accessMatrix.filter((entry) => {
			const matchesUser = !accessUserFilter || safeLower(entry.user_name).includes(accessUserFilter.toLowerCase());
			const matchesDb = !accessDatabaseFilter || safeLower(entry.database_name).includes(accessDatabaseFilter.toLowerCase());
			return matchesUser && matchesDb;
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

	let groupedOverPermissions = $derived.by<OverPermissionGroup[]>(() => {
		const byUser = new Map<string, OverPermission[]>();

		for (const alert of overPermissions) {
			const key = alert.user_name || '(unknown user)';
			const bucket = byUser.get(key);
			if (bucket) {
				bucket.push(alert);
			} else {
				byUser.set(key, [alert]);
			}
		}

		return Array.from(byUser.entries())
			.map(([userName, alerts]) => {
				const sortedAlerts = [...alerts].sort((a, b) => {
					const sevDelta =
						overPermissionSeverityPriority(overPermissionSeverity(b)) -
						overPermissionSeverityPriority(overPermissionSeverity(a));
					if (sevDelta !== 0) return sevDelta;
					const dbDelta = (a.database_name || '*').localeCompare(b.database_name || '*');
					if (dbDelta !== 0) return dbDelta;
					return a.privilege.localeCompare(b.privilege);
				});

				let critical = 0;
				let warn = 0;
				let info = 0;
				for (const alert of sortedAlerts) {
					const severity = overPermissionSeverity(alert);
					if (severity === 'critical') critical++;
					else if (severity === 'warn') warn++;
					else info++;
				}
				const topSeverity: 'critical' | 'warn' | 'info' =
					critical > 0 ? 'critical' : warn > 0 ? 'warn' : 'info';

				return {
					userName,
					alerts: sortedAlerts,
					total: sortedAlerts.length,
					databases: new Set(sortedAlerts.map((alert) => alert.database_name || '*')).size,
					critical,
					warn,
					info,
					topSeverity
				};
			})
			.sort((a, b) => {
				const sevDelta =
					overPermissionSeverityPriority(b.topSeverity) - overPermissionSeverityPriority(a.topSeverity);
				if (sevDelta !== 0) return sevDelta;
				if (a.total !== b.total) return b.total - a.total;
				return a.userName.localeCompare(b.userName);
			});
	});

	onMount(() => {
		const initialTab = normalizeGovernanceTab(
			typeof window === 'undefined' ? null : new URLSearchParams(window.location.search).get('tab')
		);
		switchTab(initialTab, true);
	});
</script>

<div class="flex flex-col h-full">
	<div class="border-b border-gray-200 dark:border-gray-800">
		<div class="flex flex-col gap-3 px-4 py-3 md:flex-row md:items-center md:justify-between">
			<div class="flex min-w-0 flex-col gap-2 md:flex-row md:items-center md:gap-4">
				<h1 class="ds-page-title">Governance</h1>
				<nav class="ds-tabs border-0 px-0 pt-0 gap-1 overflow-x-auto whitespace-nowrap" aria-label="Tabs">
					{#each governanceTabs as tab}
						<button
							onclick={() => switchTab(tab.id)}
							class={`ds-tab ${
								activeTab === tab.id
									? 'ds-tab-active'
									: ''
							}`}
						>
							{tab.label}
						</button>
					{/each}
				</nav>
			</div>
			<div>
				<button
					onclick={handleSyncNow}
					disabled={syncing}
					class="ds-btn-primary px-3 py-1.5 text-sm disabled:opacity-50 disabled:cursor-not-allowed"
				>
					<RefreshCw class={`w-4 h-4 ${syncing ? 'animate-spin' : ''}`} />

				</button>
			</div>
		</div>
	</div>

	<div class="flex-1 overflow-auto p-4">
		<div class="max-w-7xl mx-auto">
			{#if loading && !overview && !tables.length && !queryLog.length && !lineageEdges.length && !users.length && !policies.length}
				<div class="flex justify-center items-center py-12">
					<Spinner size="lg" />
				</div>
			{:else}
					<!-- Dashboard Tab -->
					{#if activeTab === 'dashboard'}
						{#if overview}
							<!-- Stats Cards -->
							<div class="grid grid-cols-2 lg:grid-cols-5 gap-3 mb-4">
								<div class="ds-panel p-3">
									<div class="flex items-center justify-between">
										<Database class="w-4 h-4 text-ch-blue" />
										<span class="text-xl font-bold text-gray-900 dark:text-white">{overview.database_count}</span>
									</div>
									<p class="text-[11px] text-gray-600 dark:text-gray-400 mt-1">Databases</p>
								</div>
								<div class="ds-panel p-3">
									<div class="flex items-center justify-between">
										<Table2 class="w-4 h-4 text-ch-blue" />
										<span class="text-xl font-bold text-gray-900 dark:text-white">{overview.table_count}</span>
									</div>
									<p class="text-[11px] text-gray-600 dark:text-gray-400 mt-1">Tables</p>
								</div>
								<div class="ds-panel p-3">
									<div class="flex items-center justify-between">
										<Columns3 class="w-4 h-4 text-ch-blue" />
										<span class="text-xl font-bold text-gray-900 dark:text-white">{overview.column_count}</span>
									</div>
									<p class="text-[11px] text-gray-600 dark:text-gray-400 mt-1">Columns</p>
								</div>
								<div class="ds-panel p-3">
									<div class="flex items-center justify-between">
										<Users class="w-4 h-4 text-ch-blue" />
										<span class="text-xl font-bold text-gray-900 dark:text-white">{overview.user_count}</span>
									</div>
									<p class="text-[11px] text-gray-600 dark:text-gray-400 mt-1">Users</p>
								</div>
								<div class="ds-panel p-3">
									<div class="flex items-center justify-between">
										<Search class="w-4 h-4 text-ch-blue" />
										<span class="text-xl font-bold text-gray-900 dark:text-white">{overview.query_count_24h}</span>
									</div>
									<p class="text-[11px] text-gray-600 dark:text-gray-400 mt-1">Queries (24h)</p>
								</div>
								<div class="ds-panel p-3">
									<div class="flex items-center justify-between">
										<GitBranch class="w-4 h-4 text-ch-blue" />
										<span class="text-xl font-bold text-gray-900 dark:text-white">{overview.lineage_edge_count}</span>
									</div>
									<p class="text-[11px] text-gray-600 dark:text-gray-400 mt-1">Lineage Edges</p>
								</div>
								<div class="ds-panel p-3">
									<div class="flex items-center justify-between">
										<Shield class="w-4 h-4 text-ch-blue" />
										<span class="text-xl font-bold text-gray-900 dark:text-white">{overview.policy_count}</span>
									</div>
									<p class="text-[11px] text-gray-600 dark:text-gray-400 mt-1">Policies</p>
								</div>
								<div class="ds-panel p-3">
									<div class="flex items-center justify-between">
										<AlertTriangle class="w-4 h-4 text-red-500" />
										<span class="text-xl font-bold text-gray-900 dark:text-white">{overview.violation_count}</span>
									</div>
									<p class="text-[11px] text-gray-600 dark:text-gray-400 mt-1">Violations</p>
								</div>
								<div class="ds-panel p-3">
									<div class="flex items-center justify-between">
										<Siren class="w-4 h-4 text-orange-500" />
										<span class="text-xl font-bold text-gray-900 dark:text-white">{overview.incident_count || 0}</span>
									</div>
									<p class="text-[11px] text-gray-600 dark:text-gray-400 mt-1">Open Incidents</p>
								</div>
								<div class="ds-panel p-3">
									<div class="flex items-center justify-between">
										<Table2 class="w-4 h-4 text-green-500" />
										<span class="text-xl font-bold text-gray-900 dark:text-white">{overview.tagged_table_count}</span>
									</div>
									<p class="text-[11px] text-gray-600 dark:text-gray-400 mt-1">Tagged Tables</p>
								</div>
							</div>

							<!-- Trends -->
							<div class="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-6">
								<div class="ds-panel p-3">
									<div class="flex items-center justify-between mb-2">
										<h3 class="text-xs font-semibold uppercase tracking-wide text-gray-600 dark:text-gray-300">
											Violations Trend (7d)
										</h3>
										<HelpTip text="uPlot sparkline of policy violations detected over the last 7 days." />
									</div>
									<MiniTrendChart x={dashboardViolationTrend.x} y={dashboardViolationTrend.y} color="#ef4444" fill="rgba(239,68,68,0.18)" height={110} />
								</div>
								<div class="ds-panel p-3">
									<div class="flex items-center justify-between mb-2">
										<h3 class="text-xs font-semibold uppercase tracking-wide text-gray-600 dark:text-gray-300">
											Schema Change Trend (7d)
										</h3>
										<HelpTip text="uPlot sparkline of metadata/schema change events over the last 7 days." />
									</div>
									<MiniTrendChart x={dashboardSchemaTrend.x} y={dashboardSchemaTrend.y} color="#10b981" fill="rgba(16,185,129,0.18)" height={110} />
								</div>
							</div>

							<!-- Sync Status -->
								<div class="ds-panel p-6 mb-8">
									<div class="flex items-center justify-between mb-4">
										<h2 class="text-lg font-semibold text-gray-900 dark:text-white">Sync Status</h2>
										<div class="text-xs text-gray-500 dark:text-gray-400">Status updates from governance sync workers</div>
									</div>
								<div class="space-y-3">
										{#each overview.sync_states ?? [] as syncState}
										<div class="flex items-center justify-between">
											<span class="text-sm font-medium text-gray-700 dark:text-gray-300">{syncState.sync_type}</span>
											<div class="flex items-center space-x-2">
												<span class="text-xs text-gray-500 dark:text-gray-400">
													{syncState.last_synced_at ? formatTime(syncState.last_synced_at) : 'Never'}
												</span>
													<span
														class={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${
															syncState.status === 'idle'
																? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
																: syncState.status === 'running'
																	? 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
																	: syncState.status === 'error'
																		? 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
																		: 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200'
														}`}
													>
														{syncStatusLabel(syncState.status)}
													</span>
											</div>
										</div>
									{/each}
								</div>
							</div>

							<!-- Recent Changes and Violations -->
							<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
								<!-- Recent Schema Changes -->
								<div class="ds-panel p-6">
									<h2 class="text-lg font-semibold text-gray-900 dark:text-white mb-4">Recent Schema Changes</h2>
									{#if overview.recent_changes && overview.recent_changes.length > 0}
										<div class="space-y-3">
											{#each overview.recent_changes as change}
												<div class="text-sm">
													<div class="flex items-center justify-between mb-1">
														<span class="font-medium text-gray-900 dark:text-white">{change.database_name}.{change.table_name}</span>
														<span class="text-xs text-gray-500">{formatTime(change.detected_at)}</span>
													</div>
													<p class="text-gray-600 dark:text-gray-400">{change.change_type}</p>
												</div>
											{/each}
										</div>
									{:else}
										<p class="text-sm text-gray-500 dark:text-gray-400">No recent changes</p>
									{/if}
								</div>

								<!-- Recent Violations -->
								<div class="ds-panel p-6">
									<h2 class="text-lg font-semibold text-gray-900 dark:text-white mb-4">Recent Violations</h2>
									{#if overview.recent_violations && overview.recent_violations.length > 0}
										<div class="space-y-3">
											{#each overview.recent_violations as violation}
												<div class="text-sm">
													<div class="flex items-center justify-between mb-1">
														<span class="font-medium text-gray-900 dark:text-white">{violation.policy_name}</span>
														<span
															class={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${
																violation.severity === 'critical'
																	? 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
																	: violation.severity === 'warn'
																		? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200'
																		: 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
															}`}
														>
															{violation.severity}
														</span>
													</div>
														<p class="text-gray-600 dark:text-gray-400">{truncate(violation.violation_detail, 60)}</p>
													<p class="text-xs text-gray-500 mt-1">{formatTime(violation.detected_at)}</p>
												</div>
											{/each}
										</div>
									{:else}
										<p class="text-sm text-gray-500 dark:text-gray-400">No recent violations</p>
									{/if}
								</div>
							</div>
						{/if}
					{/if}

					<!-- Tables Tab -->
					{#if activeTab === 'tables'}
						<div class="space-y-6">
							<!-- Filters -->
							<div class="flex flex-col md:flex-row gap-4">
								<div class="flex-1">
									<input
										type="text"
										placeholder="Search tables..."
										bind:value={tableSearchQuery}
										class="ds-input"
									/>
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

							<!-- Tables List -->
							{#if filteredTables.length > 0}
								<div class="overflow-x-auto">
									<table class="ds-table">
										<thead>
											<tr class="ds-table-head-row">
												<th class="ds-table-th">Database</th>
												<th class="ds-table-th">Table</th>
												<th class="ds-table-th">Engine</th>
												<th class="ds-table-th">Rows</th>
												<th class="ds-table-th">Size</th>
												<th class="ds-table-th">Tags</th>
												<th class="ds-table-th-right">Details</th>
											</tr>
										</thead>
										<tbody>
											{#each filteredTables as table}
												<tr class="ds-table-row">
													<td class="py-2 px-3 text-gray-800 dark:text-gray-200">{table.database_name}</td>
													<td class="py-2 px-3 text-gray-800 dark:text-gray-200 font-medium">{table.table_name}</td>
													<td class="py-2 px-3 text-gray-500 dark:text-gray-400">{table.engine}</td>
													<td class="py-2 px-3 text-gray-500 dark:text-gray-400">{table.total_rows.toLocaleString()}</td>
													<td class="py-2 px-3 text-gray-500 dark:text-gray-400">{formatBytes(table.total_bytes)}</td>
													<td class="py-2 px-3">
														{#if table.tags && table.tags.length > 0}
															<div class="flex flex-wrap gap-1">
																{#each table.tags as tag}
																	<span class="inline-flex items-center px-1.5 py-0.5 rounded border border-orange-200 bg-orange-100 text-orange-900 dark:border-orange-700/60 dark:bg-orange-500/15 dark:text-orange-200 text-[11px]">
																		{tag}
																	</span>
																{/each}
															</div>
														{:else}
															<span class="text-gray-400">-</span>
														{/if}
													</td>
														<td class="py-2 px-3 text-right">
															<button
																onclick={() => openTableDetails(table)}
																class="ds-btn-outline px-2 py-1"
															>
																<PanelRightOpen class="w-3 h-3" />
																View
														</button>
													</td>
												</tr>
											{/each}
										</tbody>
									</table>
								</div>
							{:else}
								<div class="ds-empty py-12">
									<Table2 class="w-12 h-12 mx-auto text-gray-400 mb-4" />
									<p class="text-gray-500 dark:text-gray-400">No tables found</p>
								</div>
							{/if}
						</div>
					{/if}

					<!-- Query Audit Tab -->
					{#if activeTab === 'queries'}
						<div class="space-y-6">
							<!-- Filters -->
							<div class="flex flex-col md:flex-row gap-4">
								<div class="flex-1">
									<input
										type="text"
										placeholder="Filter by user..."
										bind:value={queryUserFilter}
										class="ds-input"
									/>
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
								<button
									onclick={() => loadQueries()}
									class="ds-btn-primary px-4 py-2"
								>
									Apply Filters
								</button>
							</div>

							<!-- Top Queries -->
							{#if topQueries.length > 0}
								<div>
									<h3 class="text-sm font-semibold text-gray-700 dark:text-gray-300 mb-2">Top Queries by Execution Count</h3>
									<div class="overflow-x-auto">
										<table class="ds-table">
											<thead>
												<tr class="ds-table-head-row">
													<th class="ds-table-th">Query</th>
													<th class="ds-table-th-right">Runs</th>
												</tr>
											</thead>
											<tbody>
												{#each topQueries as tq}
													<tr class="ds-table-row">
														<td class="py-2 px-3 text-xs text-gray-500 font-mono max-w-xl truncate">{truncate(tq.sample_query, 140)}</td>
														<td class="py-2 px-3 text-right text-xs text-gray-500 whitespace-nowrap">{tq.count} runs</td>
													</tr>
												{/each}
											</tbody>
										</table>
									</div>
								</div>
							{/if}

							<!-- Query Log -->
							{#if queryLog.length > 0}
								<div class="overflow-x-auto">
									<table class="ds-table">
										<thead>
											<tr class="ds-table-head-row">
												<th class="ds-table-th">Time</th>
												<th class="ds-table-th">User</th>
												<th class="ds-table-th">Type</th>
												<th class="ds-table-th">Query</th>
												<th class="ds-table-th">Duration</th>
												<th class="ds-table-th">Rows</th>
												<th class="ds-table-th-right">Details</th>
											</tr>
										</thead>
										<tbody>
											{#each queryLog as entry}
												<tr class="ds-table-row">
													<td class="py-2 px-3 text-xs text-gray-500 whitespace-nowrap">{formatTime(entry.event_time)}</td>
													<td class="py-2 px-3 text-gray-800 dark:text-gray-200">{entry.ch_user}</td>
													<td class="py-2 px-3">
														<span
															class={`inline-flex items-center px-1.5 py-0.5 rounded text-[11px] ${
																entry.is_error
																	? 'bg-red-100 text-red-800 dark:bg-red-900/30 dark:text-red-300'
																	: 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-300'
															}`}
														>
															{entry.query_kind}
														</span>
													</td>
													<td class="py-2 px-3 text-xs text-gray-500 max-w-xs truncate font-mono">{truncate(entry.query_text, 80)}</td>
													<td class="py-2 px-3 text-xs text-gray-500 whitespace-nowrap">{entry.duration_ms}ms</td>
													<td class="py-2 px-3 text-xs text-gray-500">{entry.read_rows.toLocaleString()}</td>
														<td class="py-2 px-3 text-right">
															<button
																onclick={() => openQueryDetails(entry)}
																class="ds-btn-outline px-2 py-1"
															>
																<PanelRightOpen class="w-3 h-3" />
																View
														</button>
													</td>
												</tr>
											{/each}
										</tbody>
									</table>
								</div>
							{:else}
								<div class="ds-empty py-12">
									<Search class="w-12 h-12 mx-auto text-gray-400 mb-4" />
									<p class="text-gray-500 dark:text-gray-400">No query logs found</p>
								</div>
							{/if}
						</div>
					{/if}

					<!-- Lineage Tab -->
					{#if activeTab === 'lineage'}
						<div class="space-y-6">
							{#if lineageEdges.length > 0}
								<div class="overflow-x-auto">
									<table class="ds-table">
										<thead>
											<tr class="ds-table-head-row">
												<th class="ds-table-th">Source</th>
												<th class="ds-table-th">Type</th>
												<th class="ds-table-th">Target</th>
												<th class="ds-table-th">User</th>
												<th class="ds-table-th">Last Seen</th>
											</tr>
										</thead>
										<tbody>
											{#each lineageEdges as edge}
												<tr class="ds-table-row">
													<td class="py-2 px-3 text-gray-800 dark:text-gray-200 font-medium">{edge.source_database}.{edge.source_table}</td>
													<td class="py-2 px-3">
														<span class="inline-flex items-center px-1.5 py-0.5 rounded border border-orange-200 bg-orange-100 text-orange-900 dark:border-orange-700/60 dark:bg-orange-500/15 dark:text-orange-200 text-[11px]">
															{edge.edge_type}
														</span>
													</td>
													<td class="py-2 px-3 text-gray-800 dark:text-gray-200 font-medium">{edge.target_database}.{edge.target_table}</td>
													<td class="py-2 px-3 text-gray-500 dark:text-gray-400">{edge.ch_user}</td>
													<td class="py-2 px-3 text-xs text-gray-500 whitespace-nowrap">{formatTime(edge.detected_at)}</td>
												</tr>
											{/each}
										</tbody>
									</table>
								</div>
							{:else}
								<div class="ds-empty py-12">
									<GitBranch class="w-12 h-12 mx-auto text-gray-400 mb-4" />
									<p class="text-gray-500 dark:text-gray-400">No lineage data. Run a sync to detect data flows.</p>
								</div>
							{/if}
						</div>
					{/if}

					<!-- Access Tab -->
					{#if activeTab === 'access'}
						<div class="space-y-6">
							<!-- Over-Permissions Alerts -->
							{#if overPermissions.length > 0}
								<div class="ds-card p-6">
									<h3 class="text-lg font-semibold text-gray-900 dark:text-white mb-4 flex items-center">
										<AlertTriangle class="w-5 h-5 mr-2 text-yellow-500" />
										Over-Permissions Detected
									</h3>
									<div class="space-y-3">
										{#each groupedOverPermissions as group}
											{@const groupExpanded = expandedOverPermissionUsers[group.userName] ?? group.topSeverity === 'critical'}
											<div class={`rounded-lg border ${overPermissionPanelTone(group.topSeverity)}`}>
												<div
													role="button"
													tabindex="0"
													onclick={() => toggleOverPermissionGroup(group.userName)}
													onkeydown={(e) => {
														if (e.key === 'Enter' || e.key === ' ') {
															e.preventDefault();
															toggleOverPermissionGroup(group.userName);
														}
													}}
													class="w-full p-4 text-left flex items-center justify-between gap-3"
												>
													<div class="min-w-0 flex items-start gap-2.5">
														<ChevronRight class={`mt-0.5 w-4 h-4 shrink-0 text-gray-600 dark:text-gray-300 transition-transform ${groupExpanded ? 'rotate-90' : ''}`} />
														<div class="min-w-0">
															<p class="font-medium text-gray-900 dark:text-white truncate">{group.userName}</p>
															<p class="text-sm text-gray-600 dark:text-gray-400">
																{group.total} risky grants across {group.databases} {group.databases === 1 ? 'database' : 'databases'}
															</p>
														</div>
													</div>
													<div class="flex items-center gap-1.5 shrink-0">
														<button
															type="button"
															onclick={(e) => {
																e.stopPropagation();
																openAccessGroupDetails(group);
															}}
															class="ds-btn-outline px-2.5 py-1"
														>
															<PanelRightOpen class="w-3.5 h-3.5" />
															User Details
														</button>
														{#if group.critical > 0}
															<span class={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${overPermissionBadgeTone('critical')}`}>critical {group.critical}</span>
														{/if}
														{#if group.warn > 0}
															<span class={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${overPermissionBadgeTone('warn')}`}>warn {group.warn}</span>
														{/if}
														{#if group.info > 0}
															<span class={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${overPermissionBadgeTone('info')}`}>info {group.info}</span>
														{/if}
													</div>
												</div>

												{#if groupExpanded}
													<div class="px-4 pb-4 space-y-2 border-t border-gray-200/70 dark:border-gray-800/70">
														{#each group.alerts as alert}
															{@const severity = overPermissionSeverity(alert)}
															<div class="bg-white/70 dark:bg-black/20 rounded-lg border border-gray-200/70 dark:border-gray-800/70 p-3">
																<div class="flex items-start justify-between gap-3">
																	<div class="min-w-0">
																		<p class="text-sm text-gray-600 dark:text-gray-400">{alert.reason}</p>
																		<p class="text-xs text-gray-500 dark:text-gray-500 mt-1">Database: {alert.database_name || '*'}</p>
																		<p class="text-xs text-gray-500 dark:text-gray-500">Privilege: {alert.privilege}</p>
																	</div>
																	<div class="flex items-center gap-2 shrink-0">
																		<span class={`inline-flex items-center px-2 py-1 rounded-full text-xs font-medium ${overPermissionBadgeTone(severity)}`}>
																			{severity}
																		</span>
																		<button
																			onclick={() => openAccessDetails(alert)}
																			class="ds-btn-outline px-2.5 py-1"
																		>
																			<PanelRightOpen class="w-3.5 h-3.5" />
																			Details
																		</button>
																	</div>
																</div>
															</div>
														{/each}
													</div>
												{/if}
											</div>
										{/each}
									</div>
								</div>
							{/if}

							<!-- Users -->
							<div class="ds-card p-6">
								<h3 class="text-lg font-semibold text-gray-900 dark:text-white mb-4">Users</h3>
								{#if users.length > 0}
									<div class="overflow-x-auto">
										<table class="ds-table">
											<thead>
												<tr class="ds-table-head-row">
													<th class="ds-table-th">Name</th>
													<th class="ds-table-th">Auth Type</th>
													<th class="ds-table-th">Host</th>
													<th class="ds-table-th">Default Roles</th>
												</tr>
											</thead>
											<tbody>
												{#each users as user}
													<tr class="ds-table-row">
														<td class="py-2 px-3 text-gray-800 dark:text-gray-200 font-medium">{user.name}</td>
														<td class="py-2 px-3 text-gray-500 dark:text-gray-400">{user.auth_type || '-'}</td>
														<td class="py-2 px-3 text-gray-500 dark:text-gray-400">{user.host_ip || '-'}</td>
														<td class="py-2 px-3 text-gray-500 dark:text-gray-400">{formatDefaultRoles(user.default_roles)}</td>
													</tr>
												{/each}
											</tbody>
										</table>
									</div>
								{:else}
									<p class="text-sm text-gray-500 dark:text-gray-400">No users found</p>
								{/if}
							</div>

							<!-- Roles -->
							<div class="ds-card p-6">
								<h3 class="text-lg font-semibold text-gray-900 dark:text-white mb-4">Roles</h3>
								{#if roles.length > 0}
									<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
										{#each roles as role}
											<div class="bg-gray-50 dark:bg-gray-900 rounded-lg p-4 border border-gray-200 dark:border-gray-800">
												<p class="font-medium text-gray-900 dark:text-white">{role.name}</p>
											</div>
										{/each}
									</div>
								{:else}
									<p class="text-sm text-gray-500 dark:text-gray-400">No roles found</p>
								{/if}
							</div>

							<!-- Access Matrix -->
							<div class="ds-card p-6">
								<div class="flex items-center gap-2 mb-4">
									<h3 class="text-lg font-semibold text-gray-900 dark:text-white">Access Matrix</h3>
									<HelpTip text="Live materialized user/role grants. Use filters to narrow scope; table stays contained with vertical scroll to avoid page overflow." />
								</div>

								<!-- Filters -->
								<div class="flex flex-col md:flex-row gap-4 mb-4">
									<div class="flex-1">
										<input
											type="text"
											placeholder="Filter by user..."
											bind:value={accessUserFilter}
											class="ds-input"
										/>
									</div>
									<div class="flex-1">
										<input
											type="text"
											placeholder="Filter by database..."
											bind:value={accessDatabaseFilter}
											class="ds-input"
										/>
									</div>
								</div>

								{#if filteredAccessMatrix.length > 0}
									<div class="overflow-x-auto max-h-[60vh] overflow-y-auto rounded-lg border border-gray-200 dark:border-gray-800">
										<table class="ds-table table-fixed min-w-[900px]">
											<thead>
												<tr class="ds-table-head-row sticky top-0 z-10 bg-gray-50 dark:bg-gray-900">
													<th class="ds-table-th">User</th>
													<th class="ds-table-th">Database</th>
													<th class="ds-table-th">Table</th>
													<th class="ds-table-th">Privilege</th>
													<th class="ds-table-th">Grant Option</th>
												</tr>
											</thead>
											<tbody>
												{#each filteredAccessMatrix as entry}
													<tr class="ds-table-row">
														<td class="py-2 px-3 text-gray-800 dark:text-gray-200 font-medium truncate">{entry.user_name}</td>
														<td class="py-2 px-3 text-gray-500 dark:text-gray-400 truncate">{entry.database_name || '*'}</td>
														<td class="py-2 px-3 text-gray-500 dark:text-gray-400 truncate">{entry.table_name || '*'}</td>
														<td class="py-2 px-3">
															<span class="inline-flex max-w-full items-center px-1.5 py-0.5 rounded border border-orange-200 bg-orange-100 text-orange-900 dark:border-orange-700/60 dark:bg-orange-500/15 dark:text-orange-200 text-[11px] truncate">
																{entry.privilege}
															</span>
														</td>
														<td class="py-2 px-3 text-gray-500 dark:text-gray-400">{entry.is_direct_grant ? 'Direct' : 'Inherited'}</td>
													</tr>
												{/each}
											</tbody>
										</table>
									</div>
								{:else}
									<p class="text-sm text-gray-500 dark:text-gray-400">No access grants found</p>
								{/if}
							</div>
						</div>
					{/if}

					<!-- Incidents Tab -->
					{#if activeTab === 'incidents'}
						<div class="space-y-4">
							<div class="ds-card p-4">
								<div class="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
									<div class="flex flex-col gap-3 md:flex-row md:items-center md:gap-3">
										<div class="w-full md:w-48">
											<Combobox
												options={incidentStatusOptions}
												value={incidentStatusFilter}
												onChange={(v) => incidentStatusFilter = v}
												placeholder="All Statuses"
											/>
										</div>
										<div class="w-full md:w-48">
											<Combobox
												options={incidentSeverityOptions}
												value={incidentSeverityFilter}
												onChange={(v) => incidentSeverityFilter = v}
												placeholder="All Severities"
											/>
										</div>
										<button class="ds-btn-primary px-3 py-2" onclick={() => loadIncidents()}>Apply Filters</button>
									</div>
									<button class="ds-btn-outline px-3 py-2" onclick={() => incidentCreateSheetOpen = true}>
										<Plus class="w-3.5 h-3.5" />
										New Incident
									</button>
								</div>
							</div>

							<div class="ds-card p-4">
								<div class="flex items-center justify-between mb-3">
									<div class="flex items-center gap-2">
										<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Incident Queue</h3>
										<HelpTip text="Operational queue for governance issues. Open incidents in the sheet to triage, assign, resolve, and collaborate via comments." />
									</div>
									<span class="text-xs text-gray-500 dark:text-gray-400">{incidents.length} incidents</span>
								</div>
								{#if incidents.length > 0}
									<div class="overflow-x-auto">
										<table class="ds-table text-xs">
											<thead>
												<tr class="ds-table-head-row">
													<th class="ds-table-th">Title</th>
													<th class="ds-table-th">Severity</th>
													<th class="ds-table-th">Status</th>
													<th class="ds-table-th">Occurrences</th>
													<th class="ds-table-th">Assignee</th>
													<th class="ds-table-th">Last Seen</th>
													<th class="ds-table-th-right">Details</th>
												</tr>
											</thead>
											<tbody>
												{#each incidents as incident}
													<tr class="ds-table-row">
														<td class="px-3 py-2.5 text-gray-900 dark:text-white font-medium">{incident.title}</td>
														<td class="px-3 py-2.5">
															<span class={`inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium ${
																incident.severity === 'critical'
																	? 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
																	: incident.severity === 'error'
																		? 'bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200'
																		: incident.severity === 'warn'
																			? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200'
																			: 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
															}`}>{incident.severity}</span>
														</td>
														<td class="px-3 py-2.5 text-gray-700 dark:text-gray-300">{incident.status}</td>
														<td class="px-3 py-2.5 text-gray-700 dark:text-gray-300">{incident.occurrence_count}</td>
														<td class="px-3 py-2.5 text-gray-700 dark:text-gray-300">{incident.assignee || '-'}</td>
														<td class="px-3 py-2.5 text-gray-500 dark:text-gray-400">{formatTime(incident.last_seen_at)}</td>
														<td class="px-3 py-2.5 text-right">
															<button class="ds-btn-outline px-2.5 py-1" onclick={() => openIncidentDetails(incident)}>
																<PanelRightOpen class="w-3.5 h-3.5" />
																Open
															</button>
														</td>
													</tr>
												{/each}
											</tbody>
										</table>
									</div>
								{:else}
									<div class="text-center py-8">
										<Siren class="w-8 h-8 mx-auto text-gray-400 mb-2" />
										<p class="text-sm text-gray-500 dark:text-gray-400">No incidents found for current filters</p>
									</div>
								{/if}
							</div>
						</div>
					{/if}

					<!-- Policies Tab -->
					{#if activeTab === 'policies'}
						<div class="space-y-3">
							<!-- Policy List -->
								<div class="ds-card">
								<div class="px-4 py-3 border-b border-gray-200 dark:border-gray-700">
									<div class="flex items-center justify-between">
										<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Policies</h3>
										<button
											onclick={() => openPolicyForm()}
											class="ds-btn-primary px-3 py-1.5"
										>
											<Plus class="w-3.5 h-3.5 mr-1.5" />
											Create Policy
										</button>
									</div>
								</div>

								<div class="px-4 py-3">
									{#if policies.length > 0}
										<div class="overflow-x-auto">
											<table class="ds-table text-xs">
												<thead>
													<tr class="ds-table-head-row">
														<th class="ds-table-th">Policy</th>
														<th class="ds-table-th">Scope</th>
														<th class="ds-table-th">Role</th>
														<th class="ds-table-th">Severity</th>
														<th class="ds-table-th">Mode</th>
														<th class="ds-table-th">Status</th>
														<th class="ds-table-th">Updated</th>
														<th class="ds-table-th-right">Actions</th>
													</tr>
												</thead>
												<tbody>
													{#each policies as policy}
														<tr class="ds-table-row">
															<td class="px-3 py-2.5">
																<p class="font-semibold text-gray-900 dark:text-white">{policy.name}</p>
																{#if policy.description}
																	<p class="text-[11px] text-gray-500 dark:text-gray-400 mt-0.5">{truncate(policy.description, 70)}</p>
																{/if}
															</td>
															<td class="px-3 py-2.5 text-gray-600 dark:text-gray-300">
																{policy.object_type}{policy.object_database ? ` / ${policy.object_database}` : ''}{policy.object_table ? `.${policy.object_table}` : ''}{policy.object_column ? `.${policy.object_column}` : ''}
															</td>
															<td class="px-3 py-2.5 text-gray-600 dark:text-gray-300">{policy.required_role || '-'}</td>
															<td class="px-3 py-2.5">
																<span
																	class={`inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium ${
																		policy.severity === 'critical'
																			? 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
																			: policy.severity === 'warn'
																				? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200'
																				: 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
																	}`}
																>
																	{policy.severity}
																</span>
															</td>
															<td class="px-3 py-2.5">
																<span
																	class={`inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium ${
																		policy.enforcement_mode === 'block'
																			? 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
																			: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-200'
																	}`}
																>
																	{policy.enforcement_mode}
																</span>
															</td>
															<td class="px-3 py-2.5">
																<span
																	class={`inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium ${
																		policy.enabled
																			? 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200'
																			: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-200'
																	}`}
																>
																	{policy.enabled ? 'enabled' : 'disabled'}
																</span>
															</td>
															<td class="px-3 py-2.5 text-gray-500 dark:text-gray-400">{formatTime(policy.updated_at)}</td>
															<td class="px-3 py-2.5">
																<div class="flex justify-end gap-1">
																		<button
																			onclick={() => openPolicyForm(policy)}
																			class="ds-icon-btn"
																			title="Edit policy"
																		>
																			<Edit class="w-3.5 h-3.5" />
																		</button>
																		<button
																			onclick={() => requestDeletePolicy(policy)}
																			class="ds-icon-btn hover:text-red-500 dark:hover:text-red-400"
																			title="Delete policy"
																		>
																			<Trash2 class="w-3.5 h-3.5" />
																	</button>
																</div>
															</td>
														</tr>
													{/each}
												</tbody>
											</table>
										</div>
									{:else}
										<div class="text-center py-6">
											<Shield class="w-8 h-8 mx-auto text-gray-400 mb-2" />
											<p class="text-sm text-gray-500 dark:text-gray-400">No policies configured</p>
										</div>
									{/if}
								</div>
							</div>

							<!-- Violations -->
								<div class="ds-card">
								<div class="px-4 py-3 border-b border-gray-200 dark:border-gray-700">
									<h3 class="text-sm font-semibold text-gray-900 dark:text-white">Policy Violations</h3>
								</div>
								{#if violations.length > 0}
									<div class="overflow-x-auto">
										<table class="ds-table text-xs">
											<thead>
												<tr class="ds-table-head-row">
													<th class="ds-table-th">Policy</th>
													<th class="ds-table-th">Severity</th>
													<th class="ds-table-th">User</th>
													<th class="ds-table-th">Detail</th>
													<th class="ds-table-th">Detected</th>
													<th class="ds-table-th-right">Actions</th>
												</tr>
											</thead>
											<tbody>
												{#each violations as violation}
													<tr class="ds-table-row">
														<td class="px-3 py-2.5 text-gray-900 dark:text-white font-medium">{violation.policy_name}</td>
														<td class="px-3 py-2.5">
															<span
																class={`inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium ${
																	violation.severity === 'critical'
																		? 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200'
																		: violation.severity === 'warn'
																			? 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200'
																			: 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
																}`}
															>
																{violation.severity}
															</span>
														</td>
														<td class="px-3 py-2.5 text-gray-600 dark:text-gray-300">{violation.ch_user}</td>
														<td class="px-3 py-2.5 text-gray-600 dark:text-gray-300">{truncate(violation.violation_detail, 120)}</td>
														<td class="px-3 py-2.5 text-gray-500 dark:text-gray-400">{formatTime(violation.detected_at)}</td>
														<td class="px-3 py-2.5 text-right">
															<button class="ds-btn-outline px-2 py-1" onclick={() => createIncidentFromViolation(violation)}>
																<MessageSquare class="w-3.5 h-3.5" />
																Create Incident
															</button>
														</td>
													</tr>
												{/each}
											</tbody>
										</table>
									</div>
								{:else}
									<div class="text-center py-6">
										<AlertTriangle class="w-8 h-8 mx-auto text-gray-400 mb-2" />
										<p class="text-sm text-gray-500 dark:text-gray-400">No violations detected</p>
									</div>
								{/if}
							</div>
						</div>
						{/if}
					{/if}

					<!-- Query Log Tab -->
					{#if activeTab === 'querylog'}
						<div class="flex flex-wrap items-center gap-2 mb-3">
							<div class="w-36">
								<Combobox
									options={qlTimeRangeOptions}
									value={qlTimeRange}
									onChange={(v) => qlTimeRange = v}
								/>
							</div>
							<input
								type="text"
								placeholder="Search query or user..."
								class="ds-input-sm w-48"
								bind:value={qlSearch}
							/>
							<div class="w-36">
								<Combobox
									value={qlQueryKind}
									placeholder="All kinds"
									options={[
										{ value: '', label: 'All kinds', keywords: 'all' },
										{ value: 'Select', label: 'Select' },
										{ value: 'Insert', label: 'Insert' },
										{ value: 'Create', label: 'Create' },
										{ value: 'Alter', label: 'Alter' },
										{ value: 'Drop', label: 'Drop' },
									]}
									onChange={(v) => qlQueryKind = v}
								/>
							</div>
							<div class="w-32">
								<Combobox
									value={qlStatus}
									placeholder="All status"
									options={[
										{ value: '', label: 'All status', keywords: 'all' },
										{ value: 'success', label: 'Success' },
										{ value: 'error', label: 'Error' },
									]}
									onChange={(v) => qlStatus = v}
								/>
							</div>
							<button
								class="ds-btn-primary"
								onclick={() => { qlOffset = 0; loadQueryLog() }}
							>Search</button>
							<button
								class="ds-btn-ghost"
								onclick={() => loadQueryLog()}
								title="Refresh"
							>
								<RefreshCw size={14} />
							</button>
						</div>

						{#if queryLogLoading}
							<div class="flex items-center justify-center py-12"><Spinner /></div>
						{:else if queryLogData.length === 0}
							<p class="text-center text-sm text-gray-500 py-8">No query log entries found</p>
						{:else}
							<div class="ds-table-wrap">
								<table class="ds-table">
									<thead>
										<tr class="ds-table-head-row">
											<th class="w-6"></th>
											<th class="ds-table-th">Time</th>
											<th class="ds-table-th">User</th>
											<th class="ds-table-th">Query</th>
											<th class="ds-table-th-right">Duration</th>
											<th class="ds-table-th-right">Rows</th>
											<th class="ds-table-th">Status</th>
										</tr>
									</thead>
									<tbody>
										{#each queryLogData as row, i}
											<tr
												class="ds-table-row cursor-pointer"
												onclick={() => expandedRow = expandedRow === i ? null : i}
											>
												<td class="py-2 px-1 text-gray-400">
													{#if expandedRow === i}<ChevronDown size={12} />{:else}<ChevronRight size={12} />{/if}
												</td>
												<td class="ds-td-mono whitespace-nowrap">{formatTime(row.event_time)}</td>
												<td class="ds-td-mono">{row.user}</td>
												<td class="ds-td-mono max-w-xs truncate">{truncate(row.query ?? '', 60)}</td>
												<td class="ds-td-right whitespace-nowrap">{row.query_duration_ms}ms</td>
												<td class="ds-td-right">{row.read_rows ?? 0}</td>
												<td class="ds-td">
													{#if row.exception_code === 0}
														<span class="ds-badge ds-badge-success">OK</span>
													{:else}
														<span class="ds-badge ds-badge-danger">Error</span>
													{/if}
												</td>
											</tr>
											{#if expandedRow === i}
												<tr class="bg-gray-50 dark:bg-gray-900/50 border-b border-gray-100 dark:border-gray-900">
													<td colspan="7" class="p-3">
														<pre class="text-xs text-gray-700 dark:text-gray-300 font-mono whitespace-pre-wrap break-all rounded p-3 border border-gray-200 dark:border-gray-800 bg-gray-100 dark:bg-gray-900">{row.query}</pre>
														{#if row.exception}
															<p class="text-xs text-red-500 mt-2"><strong>Error:</strong> {row.exception}</p>
														{/if}
													</td>
												</tr>
											{/if}
										{/each}
									</tbody>
								</table>
							</div>

							<div class="flex items-center justify-between mt-3">
								<span class="text-xs text-gray-500">Showing {qlOffset + 1}–{qlOffset + queryLogData.length}</span>
								<div class="flex gap-2">
									<button
										class="ds-btn-outline disabled:opacity-50"
										disabled={qlOffset === 0}
										onclick={() => { qlOffset = Math.max(0, qlOffset - qlLimit); loadQueryLog() }}
									>Prev</button>
									<button
										class="ds-btn-outline disabled:opacity-50"
										disabled={queryLogData.length < qlLimit}
										onclick={() => { qlOffset += qlLimit; loadQueryLog() }}
									>Next</button>
								</div>
							</div>
						{/if}
					{/if}

					<!-- Alerts Tab -->
					{#if activeTab === 'alerts'}
						{#if alertsLoading}
							<div class="flex items-center justify-center py-12"><Spinner /></div>
						{:else}
							<div class="flex flex-col md:flex-row md:items-center md:justify-between gap-3 mb-4">
								<div class="flex items-center gap-2">
									<Bell size={16} class="text-ch-blue" />
									<h2 class="text-sm font-semibold text-gray-700 dark:text-gray-300">Alerting Control Center</h2>
									<HelpTip text="Define delivery channels and route policies. Complex create flows live in sheets to keep this page clean and easy to scan." />
								</div>
								<div class="flex flex-wrap items-center gap-2">
									<button class="ds-btn-outline" onclick={() => channelSheetOpen = true}>New Channel</button>
									<button class="ds-btn-outline" onclick={() => ruleSheetOpen = true}>New Rule</button>
									<button class="ds-btn-outline" onclick={() => loadAlertsAdmin()} title="Refresh">
										<RefreshCw size={14} />
									</button>
								</div>
							</div>

							<div class="ds-card p-3 mb-4">
								<div class="flex flex-col md:flex-row gap-2 md:items-center md:justify-between">
									<div class="flex items-center gap-2">
										<span class="text-xs text-gray-500">Test recipients</span>
										<HelpTip text="Used by the Test action on each channel. Enter comma-separated emails once, then test quickly." />
									</div>
									<div class="w-full md:w-[520px]">
										<input class="ds-input-sm" placeholder="email1@company.com, email2@company.com" bind:value={alertTestRecipients} />
									</div>
								</div>
							</div>

							<div class="ds-card p-3 mb-4">
								<div class="flex items-center gap-2 mb-2">
									<h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Channels</h3>
									<HelpTip text="Channels are provider credentials (SMTP, Resend, Brevo). Routes reference these channels for delivery." />
								</div>
								{#if alertChannels.length === 0}
									<p class="text-sm text-gray-500 py-4">No alert channels configured.</p>
								{:else}
									<div class="ds-table-wrap max-h-[30vh] overflow-auto rounded border border-gray-200 dark:border-gray-800">
										<table class="ds-table">
											<thead>
												<tr class="ds-table-head-row sticky top-0 bg-gray-50 dark:bg-gray-900 z-10">
													<th class="ds-table-th">Name</th>
													<th class="ds-table-th">Type</th>
													<th class="ds-table-th">Active</th>
													<th class="ds-table-th">Secret</th>
													<th class="ds-table-th-right">Actions</th>
												</tr>
											</thead>
											<tbody>
												{#each alertChannels as channel}
													<tr class="ds-table-row">
														<td class="ds-td-strong">{channel.name}</td>
														<td class="ds-td-mono">{channel.channel_type}</td>
														<td class="ds-td">
															<input
																type="checkbox"
																class="ds-checkbox"
																checked={channel.is_active}
																onchange={(e) => toggleAlertChannel(channel, (e.target as HTMLInputElement).checked)}
															/>
														</td>
														<td class="ds-td">{channel.has_secret ? 'Configured' : 'Missing'}</td>
														<td class="ds-td-right">
															<div class="flex justify-end gap-2">
																<button class="ds-btn-outline" onclick={() => testAlertChannelRecord(channel)}>Test</button>
																<button class="text-xs text-red-500 hover:text-red-700" onclick={() => deleteAlertChannelRecord(channel)}>Delete</button>
															</div>
														</td>
													</tr>
												{/each}
											</tbody>
										</table>
									</div>
								{/if}
							</div>

							<div class="ds-card p-3 mb-4">
								<div class="flex items-center gap-2 mb-2">
									<h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Rules</h3>
									<HelpTip text="Each rule watches event types/severity and contains one or more routes. Expand a rule to inspect recipients, digest windows, and escalation." />
								</div>
								{#if alertRules.length === 0}
									<p class="text-sm text-gray-500 py-4">No alert rules configured.</p>
								{:else}
									<div class="space-y-2">
										{#each alertRules as rule}
											<details class="ds-card overflow-hidden">
												<summary class="cursor-pointer list-none px-3 py-2.5 flex items-center justify-between bg-gray-50 dark:bg-gray-900">
													<div class="min-w-0">
														<p class="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate">{rule.name}</p>
														<p class="text-[11px] text-gray-500">{rule.event_type} · min {rule.severity_min} · {rule.routes.length} routes</p>
													</div>
													<div class="flex items-center gap-2">
														<label class="ds-checkbox-label text-xs">
															<input
																type="checkbox"
																class="ds-checkbox"
																checked={rule.enabled}
																onchange={(e) => toggleAlertRule(rule, (e.target as HTMLInputElement).checked)}
															/>
															Enabled
														</label>
														<button class="text-xs text-red-500 hover:text-red-700" onclick={(e) => { e.preventDefault(); deleteAlertRuleRecord(rule) }}>Delete</button>
													</div>
												</summary>
												<div class="border-t border-gray-200 dark:border-gray-800 p-3">
													<div class="grid grid-cols-2 md:grid-cols-4 gap-2 mb-3 text-xs">
														<div class="ds-panel-muted p-2"><span class="text-gray-500">Cooldown</span><div class="font-medium text-gray-800 dark:text-gray-200">{rule.cooldown_seconds}s</div></div>
														<div class="ds-panel-muted p-2"><span class="text-gray-500">Max Attempts</span><div class="font-medium text-gray-800 dark:text-gray-200">{rule.max_attempts}</div></div>
														<div class="ds-panel-muted p-2"><span class="text-gray-500">Subject Template</span><div class="font-medium text-gray-800 dark:text-gray-200 truncate">{rule.subject_template || 'Default'}</div></div>
														<div class="ds-panel-muted p-2"><span class="text-gray-500">Body Template</span><div class="font-medium text-gray-800 dark:text-gray-200 truncate">{rule.body_template || 'Default'}</div></div>
													</div>
													<div class="overflow-x-auto rounded border border-gray-200 dark:border-gray-800">
														<table class="ds-table text-xs min-w-[980px]">
															<thead>
																<tr class="ds-table-head-row bg-gray-50 dark:bg-gray-900">
																	<th class="ds-table-th">Channel</th>
																	<th class="ds-table-th">Recipients</th>
																	<th class="ds-table-th">Delivery</th>
																	<th class="ds-table-th">Escalation</th>
																	<th class="ds-table-th">Active</th>
																</tr>
															</thead>
															<tbody>
																{#each rule.routes as route}
																	<tr class="ds-table-row">
																		<td class="ds-td-mono">{route.channel_name} ({route.channel_type})</td>
																		<td class="ds-td-mono truncate max-w-xs">{route.recipients.join(', ')}</td>
																		<td class="ds-td-mono">{route.delivery_mode}{route.delivery_mode === 'digest' ? ` (${route.digest_window_minutes}m)` : ''}</td>
																		<td class="ds-td-mono">
																			{#if route.escalation_channel_name}
																				{route.escalation_channel_name}
																				{#if route.escalation_after_failures > 0}
																					<span class="text-gray-500"> after {route.escalation_after_failures} fail</span>
																				{/if}
																			{:else}
																				—
																			{/if}
																		</td>
																		<td class="ds-td">{route.is_active ? 'yes' : 'no'}</td>
																	</tr>
																{/each}
															</tbody>
														</table>
													</div>
												</div>
											</details>
										{/each}
									</div>
								{/if}
							</div>

							<div class="ds-card p-3">
								<div class="flex items-center gap-2 mb-2">
									<h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Recent Alert Events</h3>
									<div class="w-28">
										<input class="ds-input-sm" type="number" bind:value={alertEventLimit} />
									</div>
									<button class="ds-btn-outline" onclick={() => loadAlertsAdmin()}>Refresh</button>
								</div>
								{#if alertEvents.length === 0}
									<p class="text-sm text-gray-500 py-4">No alert events yet.</p>
								{:else}
									<div class="ds-table-wrap max-h-[30vh] overflow-auto rounded border border-gray-200 dark:border-gray-800">
										<table class="ds-table">
											<thead>
												<tr class="ds-table-head-row sticky top-0 bg-gray-50 dark:bg-gray-900 z-10">
													<th class="ds-table-th">Time</th>
													<th class="ds-table-th">Type</th>
													<th class="ds-table-th">Severity</th>
													<th class="ds-table-th">Title</th>
													<th class="ds-table-th">Status</th>
												</tr>
											</thead>
											<tbody>
												{#each alertEvents as evt}
													<tr class="ds-table-row">
														<td class="ds-td-mono">{formatTime(evt.created_at)}</td>
														<td class="ds-td-mono">{evt.event_type}</td>
														<td class="ds-td-mono">{evt.severity}</td>
														<td class="ds-td">{evt.title}</td>
														<td class="ds-td-mono">{evt.status}</td>
													</tr>
												{/each}
											</tbody>
										</table>
									</div>
								{/if}
							</div>
						{/if}
					{/if}

					<!-- Audit Log Tab -->
					{#if activeTab === 'auditlog'}
						<div class="flex flex-wrap items-center gap-2 mb-3">
							<div class="w-32">
								<Combobox
									options={auditLimitOptions}
									value={String(auditLimit)}
									onChange={(v) => { auditLimit = Number(v) || 100; void loadAuditLogs() }}
								/>
							</div>
							<div class="w-36">
								<Combobox
									options={auditTimeRangeOptions}
									value={auditTimeRange}
									placeholder="All time"
									onChange={(v) => auditTimeRange = v}
								/>
							</div>
							<input
								type="text"
								placeholder="Search action, user, details, IP..."
								class="ds-input-sm w-64"
								bind:value={auditSearch}
							/>
							<div class="w-48">
								<Combobox
									options={auditActionOptions}
									value={auditAction}
									placeholder="All actions"
									onChange={(v) => auditAction = v}
								/>
							</div>
							<div class="w-40">
								<Combobox
									options={auditUsernameOptions}
									value={auditUsername}
									placeholder="All users"
									onChange={(v) => auditUsername = v}
								/>
							</div>
							<button
								class="ds-btn-primary"
								onclick={() => loadAuditLogs()}
							>Search</button>
							<button
								class="ds-btn-ghost"
								onclick={() => loadAuditLogs()}
								title="Refresh"
							>
								<RefreshCw size={14} />
							</button>
						</div>

						{#if auditLoading}
							<div class="flex items-center justify-center py-12"><Spinner /></div>
						{:else if auditLogs.length === 0}
							<p class="text-center text-sm text-gray-500 py-8">No audit logs found</p>
						{:else}
							<div class="ds-table-wrap">
								<table class="ds-table">
									<thead>
										<tr class="ds-table-head-row">
											<th class="ds-table-th">Timestamp</th>
											<th class="ds-table-th">Action</th>
											<th class="ds-table-th">User</th>
											<th class="ds-table-th">Details</th>
											<th class="ds-table-th">IP</th>
										</tr>
									</thead>
									<tbody>
										{#each auditLogs as log}
											<tr class="ds-table-row">
												<td class="ds-td-mono whitespace-nowrap">{formatTime(log.created_at)}</td>
												<td class="ds-td">
													<span class="ds-badge ds-badge-neutral font-mono">{log.action}</span>
												</td>
												<td class="ds-td-mono">{log.username ?? '—'}</td>
												<td class="ds-td-mono max-w-xs truncate">{log.details ?? '—'}</td>
												<td class="ds-td-mono">{log.ip_address ?? '—'}</td>
											</tr>
										{/each}
									</tbody>
								</table>
							</div>
						{/if}
					{/if}

			</div>
		</div>
	</div>

	<Sheet
		open={tableDetailSheetOpen}
		title={selectedTable ? `Table Details · ${selectedTable.database_name}.${selectedTable.table_name}` : 'Table Details'}
		size="xl"
		onclose={closeTableDetails}
	>
		{#if selectedTable}
			<div class="space-y-5">
				<div class="grid grid-cols-2 lg:grid-cols-4 gap-3">
					<div class="ds-panel-muted p-3">
						<p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Engine</p>
						<p class="text-sm font-medium text-gray-900 dark:text-white mt-1">{selectedTable.engine || '-'}</p>
					</div>
					<div class="ds-panel-muted p-3">
						<p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Rows</p>
						<p class="text-sm font-medium text-gray-900 dark:text-white mt-1">{selectedTable.total_rows.toLocaleString()}</p>
					</div>
					<div class="ds-panel-muted p-3">
						<p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Size</p>
						<p class="text-sm font-medium text-gray-900 dark:text-white mt-1">{formatBytes(selectedTable.total_bytes)}</p>
					</div>
					<div class="ds-panel-muted p-3">
						<p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Partitions</p>
						<p class="text-sm font-medium text-gray-900 dark:text-white mt-1">{selectedTable.partition_count}</p>
					</div>
				</div>

				<div class="ds-card overflow-hidden">
					<div class="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-800 bg-gray-50 dark:bg-gray-900">
						<h4 class="text-sm font-semibold text-gray-900 dark:text-white">Columns</h4>
						<span class="text-xs text-gray-500 dark:text-gray-400">{selectedTableColumns.length} total</span>
					</div>
					{#if tableDetailLoading}
						<div class="flex justify-center py-10">
							<Spinner size="md" />
						</div>
					{:else if selectedTableColumns.length > 0}
						<div class="overflow-x-auto">
							<table class="ds-table">
								<thead>
									<tr class="border-b border-gray-200 dark:border-gray-800 bg-gray-50 dark:bg-gray-900">
										<th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Column</th>
										<th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Type</th>
										<th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Default</th>
										<th class="px-4 py-2 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase">Tags</th>
									</tr>
								</thead>
								<tbody>
									{#each selectedTableColumns as col}
										<tr class="border-b border-gray-100 dark:border-gray-900">
											<td class="px-4 py-2 font-medium text-gray-900 dark:text-white">{col.column_name}</td>
											<td class="px-4 py-2 text-gray-600 dark:text-gray-300">{col.column_type}</td>
											<td class="px-4 py-2 text-gray-500 dark:text-gray-400">{col.default_expression || '-'}</td>
											<td class="px-4 py-2">
												{#if col.tags?.length}
													<div class="flex flex-wrap gap-1">
														{#each col.tags as tag}
															<span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium border border-orange-200 bg-orange-100 text-orange-900 dark:border-orange-700/60 dark:bg-orange-500/15 dark:text-orange-200">
																{tag}
															</span>
														{/each}
													</div>
												{:else}
													<span class="text-gray-400">-</span>
												{/if}
											</td>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{:else}
						<p class="px-4 py-8 text-sm text-center text-gray-500 dark:text-gray-400">No column metadata available.</p>
					{/if}
				</div>

				<div class="ds-card overflow-hidden">
					<div class="px-4 py-3 border-b border-gray-200 dark:border-gray-800 bg-gray-50 dark:bg-gray-900">
						<h4 class="text-sm font-semibold text-gray-900 dark:text-white">Governance Notes</h4>
					</div>
					<div class="p-4 space-y-3">
						<div class="flex gap-2">
							<textarea
								class="ds-textarea flex-1"
								rows="2"
								placeholder="Add governance note for this table (owners, SLA, sensitivity, remediation steps)..."
								bind:value={tableNoteDraft}
							></textarea>
							<button class="ds-btn-primary px-3 py-2 h-fit" onclick={() => addTableNote()}>
								<Plus class="w-3.5 h-3.5" />
								Add
							</button>
						</div>

						{#if tableNotes.length > 0}
							<div class="space-y-2 max-h-64 overflow-auto">
								{#each tableNotes as note}
									<div class="rounded-lg border border-gray-200 dark:border-gray-800 p-3 bg-white dark:bg-gray-950">
										<div class="flex items-start justify-between gap-3">
											<div class="min-w-0">
												<p class="text-sm text-gray-800 dark:text-gray-200 whitespace-pre-wrap">{note.comment_text}</p>
												<p class="text-xs text-gray-500 dark:text-gray-400 mt-1">
													{note.created_by || 'unknown'} · {formatTime(note.created_at)}
												</p>
											</div>
											<button class="ds-icon-btn hover:text-red-500" title="Delete note" onclick={() => deleteTableNote(note.id)}>
												<Trash2 class="w-3.5 h-3.5" />
											</button>
										</div>
									</div>
								{/each}
							</div>
						{:else}
							<p class="text-sm text-gray-500 dark:text-gray-400">No table notes yet.</p>
						{/if}
					</div>
				</div>
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
				<div class="grid grid-cols-2 lg:grid-cols-4 gap-3">
					<div class="ds-panel-muted p-3">
						<p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Kind</p>
						<p class="text-sm font-medium text-gray-900 dark:text-white mt-1">{selectedQuery.query_kind}</p>
					</div>
					<div class="ds-panel-muted p-3">
						<p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Duration</p>
						<p class="text-sm font-medium text-gray-900 dark:text-white mt-1">{selectedQuery.duration_ms} ms</p>
					</div>
					<div class="ds-panel-muted p-3">
						<p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Read Rows</p>
						<p class="text-sm font-medium text-gray-900 dark:text-white mt-1">{selectedQuery.read_rows.toLocaleString()}</p>
					</div>
					<div class="ds-panel-muted p-3">
						<p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Timestamp</p>
						<p class="text-sm font-medium text-gray-900 dark:text-white mt-1">{formatTime(selectedQuery.event_time)}</p>
					</div>
				</div>

				<div class="ds-card overflow-hidden">
					<div class="px-4 py-3 border-b border-gray-200 dark:border-gray-800 bg-gray-50 dark:bg-gray-900">
						<h4 class="text-sm font-semibold text-gray-900 dark:text-white">SQL</h4>
					</div>
					<pre class="p-4 text-xs font-mono text-gray-800 dark:text-gray-200 bg-white dark:bg-gray-950 overflow-x-auto">{selectedQuery.query_text}</pre>
				</div>

				{#if selectedQuery.error_message}
					<div class="rounded-lg border border-red-200 dark:border-red-800 bg-red-50 dark:bg-red-900/20 p-4">
						<p class="text-xs font-medium uppercase tracking-wide text-red-700 dark:text-red-300 mb-1">Error</p>
						<p class="text-sm text-red-800 dark:text-red-200">{selectedQuery.error_message}</p>
					</div>
				{/if}
			</div>
		{/if}
	</Sheet>

	<Sheet
		open={accessDetailSheetOpen}
		title={selectedOverPermissionGroup
			? `Permission Details · ${selectedOverPermissionGroup.userName}`
			: selectedOverPermission
				? `Permission Details · ${selectedOverPermission.user_name}`
				: 'Permission Details'}
		size="md"
		onclose={closeAccessDetails}
	>
		{#if selectedOverPermissionGroup}
			<div class="space-y-4">
				<div class="grid grid-cols-3 gap-2">
					<div class="ds-panel-muted p-3">
						<p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Total Grants</p>
						<p class="text-base font-semibold text-gray-900 dark:text-white mt-1">{selectedOverPermissionGroup.total}</p>
					</div>
					<div class="ds-panel-muted p-3">
						<p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Databases</p>
						<p class="text-base font-semibold text-gray-900 dark:text-white mt-1">{selectedOverPermissionGroup.databases}</p>
					</div>
					<div class="ds-panel-muted p-3">
						<p class="text-[11px] uppercase tracking-wide text-gray-500 dark:text-gray-400">Top Severity</p>
						<p class="text-base font-semibold text-gray-900 dark:text-white mt-1">{selectedOverPermissionGroup.topSeverity}</p>
					</div>
				</div>

				<div class="ds-card overflow-hidden">
					<div class="px-3 py-2 border-b border-gray-200 dark:border-gray-800 bg-gray-50 dark:bg-gray-900">
						<h4 class="text-sm font-semibold text-gray-900 dark:text-white">Risky Grants</h4>
					</div>
					<div class="max-h-[56vh] overflow-auto">
						<table class="ds-table text-xs">
							<thead>
								<tr class="ds-table-head-row">
									<th class="ds-table-th-compact">Database</th>
									<th class="ds-table-th-compact">Privilege</th>
									<th class="ds-table-th-compact">Reason</th>
									<th class="ds-table-th-right-compact">Details</th>
								</tr>
							</thead>
							<tbody>
								{#each selectedOverPermissionGroup.alerts as alert}
									<tr class="ds-table-row">
										<td class="ds-td-compact-strong">{alert.database_name || '*'}</td>
										<td class="ds-td-compact text-gray-700 dark:text-gray-300">{alert.privilege}</td>
										<td class="ds-td-compact">{truncate(alert.reason, 80)}</td>
										<td class="ds-td-right">
											<button
												class="ds-btn-outline px-2 py-0.5"
												onclick={() => openAccessDetails(alert)}
											>
												Open
											</button>
										</td>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				</div>
			</div>
		{:else if selectedOverPermission}
			<div class="space-y-4">
				<div class="ds-panel-muted p-4">
					<p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2">Grant Scope</p>
					<p class="text-sm text-gray-900 dark:text-white">
						{selectedOverPermission.user_name} · {selectedOverPermission.database_name || '*'}.
						{selectedOverPermission.table_name || '*'} · {selectedOverPermission.privilege}
					</p>
				</div>
				<div class="ds-panel-muted p-4">
					<p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2">Reason</p>
					<p class="text-sm text-gray-700 dark:text-gray-300">{selectedOverPermission.reason}</p>
				</div>
				<div class="ds-panel-muted p-4">
					<p class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400 mb-2">Last Query Activity</p>
					<p class="text-sm text-gray-700 dark:text-gray-300">
						{selectedOverPermission.last_query_time
							? `${formatTime(selectedOverPermission.last_query_time)} (${selectedOverPermission.days_since_query ?? 0} days ago)`
							: 'No query usage found'}
					</p>
				</div>
			</div>
		{/if}
	</Sheet>

	<Sheet
		open={incidentCreateSheetOpen}
		title="Create Incident"
		size="lg"
		onclose={() => incidentCreateSheetOpen = false}
	>
		<div class="space-y-3">
			<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
				<div>
					<div class="ds-form-label">Title</div>
					<input class="ds-input" bind:value={incidentForm.title} placeholder="Incident title" />
				</div>
				<div>
					<div class="ds-form-label">Assignee</div>
					<input class="ds-input" bind:value={incidentForm.assignee} placeholder="Optional assignee" />
				</div>
			</div>
			<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
				<div>
					<div class="ds-form-label">Severity</div>
					<Combobox
						options={incidentSeverityOptions.filter((o) => o.value)}
						value={incidentForm.severity}
						onChange={(v) => incidentForm.severity = v}
						placeholder="Severity"
					/>
				</div>
				<div>
					<div class="ds-form-label">Status</div>
					<Combobox
						options={incidentStatusOptions.filter((o) => o.value)}
						value={incidentForm.status}
						onChange={(v) => incidentForm.status = v}
						placeholder="Status"
					/>
				</div>
			</div>
			<div>
				<div class="ds-form-label">Details</div>
				<textarea class="ds-textarea" rows="4" bind:value={incidentForm.details} placeholder="What happened and what action is needed"></textarea>
			</div>
			<div class="flex justify-end">
				<button class="ds-btn-primary px-3 py-2" onclick={() => createManualIncident()}>Create Incident</button>
			</div>
		</div>
	</Sheet>

	<Sheet
		open={incidentDetailSheetOpen}
		title={selectedIncident ? `Incident · ${selectedIncident.title}` : 'Incident'}
		size="lg"
		onclose={closeIncidentDetails}
	>
		{#if selectedIncident}
			<div class="space-y-4">
				<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
					<div>
						<div class="ds-form-label">Title</div>
						<input class="ds-input" bind:value={selectedIncident.title} />
					</div>
					<div>
						<div class="ds-form-label">Assignee</div>
						<input
							class="ds-input"
							value={selectedIncident.assignee ?? ''}
							oninput={(e) => selectedIncident && (selectedIncident.assignee = (e.currentTarget as HTMLInputElement).value)}
						/>
					</div>
				</div>
				<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
					<div>
						<div class="ds-form-label">Severity</div>
						<Combobox
							options={incidentSeverityOptions.filter((o) => o.value)}
							value={selectedIncident.severity}
							onChange={(v) => selectedIncident && (selectedIncident.severity = v)}
							placeholder="Severity"
						/>
					</div>
					<div>
						<div class="ds-form-label">Status</div>
						<Combobox
							options={incidentStatusOptions.filter((o) => o.value)}
							value={selectedIncident.status}
							onChange={(v) => selectedIncident && (selectedIncident.status = v)}
							placeholder="Status"
						/>
					</div>
				</div>
				<div>
					<div class="ds-form-label">Details</div>
					<textarea
						class="ds-textarea"
						rows="3"
						value={selectedIncident.details ?? ''}
						oninput={(e) => selectedIncident && (selectedIncident.details = (e.currentTarget as HTMLTextAreaElement).value)}
					></textarea>
				</div>
				<div>
					<div class="ds-form-label">Resolution Note</div>
					<textarea
						class="ds-textarea"
						rows="2"
						value={selectedIncident.resolution_note ?? ''}
						oninput={(e) => selectedIncident && (selectedIncident.resolution_note = (e.currentTarget as HTMLTextAreaElement).value)}
					></textarea>
				</div>
				<div class="flex justify-end">
					<button class="ds-btn-primary px-3 py-2" onclick={() => saveIncidentUpdates()}>Save Incident</button>
				</div>

				<div class="ds-card overflow-hidden">
					<div class="px-4 py-3 border-b border-gray-200 dark:border-gray-800 bg-gray-50 dark:bg-gray-900">
						<h4 class="text-sm font-semibold text-gray-900 dark:text-white">Comments</h4>
					</div>
					<div class="p-4 space-y-3">
						<div class="flex gap-2">
							<textarea class="ds-textarea flex-1" rows="2" bind:value={incidentCommentDraft} placeholder="Add incident comment"></textarea>
							<button class="ds-btn-primary px-3 py-2 h-fit" onclick={() => addIncidentComment()}>
								<Plus class="w-3.5 h-3.5" />
								Add
							</button>
						</div>
						{#if incidentComments.length > 0}
							<div class="space-y-2 max-h-72 overflow-auto">
								{#each incidentComments as comment}
									<div class="rounded-lg border border-gray-200 dark:border-gray-800 p-3 bg-white dark:bg-gray-950">
										<p class="text-sm text-gray-800 dark:text-gray-200 whitespace-pre-wrap">{comment.comment_text}</p>
										<p class="text-xs text-gray-500 dark:text-gray-400 mt-1">{comment.created_by || 'unknown'} · {formatTime(comment.created_at)}</p>
									</div>
								{/each}
							</div>
						{:else}
							<p class="text-sm text-gray-500 dark:text-gray-400">No comments yet.</p>
						{/if}
					</div>
				</div>
			</div>
		{/if}
	</Sheet>

	<Sheet
		open={showPolicyForm}
		title={editingPolicy ? 'Edit Policy' : 'Create Policy'}
		size="lg"
		onclose={closePolicyForm}
	>
		<form
			onsubmit={(e) => {
				e.preventDefault();
				handlePolicySubmit();
			}}
			class="space-y-4"
		>
			<div>
				<label for="policy-name" class="ds-form-label">Name</label>
				<input
					id="policy-name"
					type="text"
					bind:value={policyForm.name}
					required
					class="ds-input"
				/>
			</div>

			<div>
				<label for="policy-description" class="ds-form-label">Description</label>
					<textarea
						id="policy-description"
						bind:value={policyForm.description}
						rows="3"
						class="ds-textarea"
					></textarea>
			</div>

			<div class="grid grid-cols-1 md:grid-cols-2 gap-4">
				<div>
					<label for="policy-object-type" class="ds-form-label">Object Type</label>
					<Combobox
						options={policyObjectTypeOptions}
						value={policyForm.object_type}
						onChange={(v) => policyForm.object_type = v as Policy['object_type']}
						placeholder="Object Type"
					/>
				</div>
				<div>
					<label for="policy-required-role" class="ds-form-label">Required Role</label>
					<input
						id="policy-required-role"
						type="text"
						bind:value={policyForm.required_role}
						placeholder="e.g. analyst"
						class="ds-input"
					/>
				</div>
			</div>

			<div class="grid grid-cols-1 md:grid-cols-3 gap-4">
				<div>
					<label for="policy-database" class="ds-form-label">Database</label>
					<input
						id="policy-database"
						type="text"
						bind:value={policyForm.object_database}
						placeholder="Optional"
						class="ds-input"
					/>
				</div>
				<div>
					<label for="policy-table" class="ds-form-label">Table</label>
					<input
						id="policy-table"
						type="text"
						bind:value={policyForm.object_table}
						placeholder="Optional"
						class="ds-input"
					/>
				</div>
				<div>
					<label for="policy-column" class="ds-form-label">Column</label>
					<input
						id="policy-column"
						type="text"
						bind:value={policyForm.object_column}
						placeholder="Optional"
						class="ds-input"
					/>
				</div>
			</div>

			<div class="grid grid-cols-1 md:grid-cols-3 gap-4">
				<div>
					<label for="policy-severity" class="ds-form-label">Severity</label>
					<Combobox
						options={policySeverityOptions}
						value={policyForm.severity}
						onChange={(v) => policyForm.severity = v}
						placeholder="Severity"
					/>
				</div>
				<div>
					<label for="policy-enforcement-mode" class="ds-form-label">Mode</label>
					<Combobox
						options={policyEnforcementModeOptions}
						value={policyForm.enforcement_mode}
						onChange={(v) => policyForm.enforcement_mode = v as Policy['enforcement_mode']}
						placeholder="Mode"
					/>
				</div>
				<label class="ds-panel-muted flex items-center gap-2 px-3 py-2 mt-6">
					<input
						type="checkbox"
						id="policy-enabled"
						bind:checked={policyForm.enabled}
						class="ds-checkbox"
					/>
					<span class="text-sm text-gray-700 dark:text-gray-300">Enabled</span>
				</label>
			</div>

			<div class="flex justify-end gap-3 pt-2">
				<button
					type="button"
					onclick={closePolicyForm}
					class="ds-btn-outline px-4 py-2"
				>
					Cancel
				</button>
				<button
					type="submit"
					disabled={loading}
					class="ds-btn-primary px-4 py-2 disabled:opacity-50 disabled:cursor-not-allowed"
				>
					{loading ? 'Saving...' : editingPolicy ? 'Update Policy' : 'Create Policy'}
				</button>
			</div>
		</form>
	</Sheet>

<ConfirmDialog
	open={confirmPolicyDeleteOpen}
	title="Delete policy?"
	description={pendingPolicyDelete ? `Delete "${pendingPolicyDelete.name}"? This action cannot be undone.` : 'This action cannot be undone.'}
	confirmLabel="Delete"
	destructive={true}
	loading={confirmPolicyDeleteLoading}
	onconfirm={confirmDeletePolicy}
	oncancel={cancelDeletePolicy}
/>

<Sheet
	open={channelSheetOpen}
	title="Create Alert Channel"
	size="xl"
	onclose={() => channelSheetOpen = false}
>
	<form
		class="space-y-4"
		onsubmit={(e) => {
			e.preventDefault();
			void createAlertChannelRecord();
		}}
	>
		<div class="flex items-center gap-2">
			<p class="text-xs text-gray-500">Channels hold delivery credentials for alert notifications.</p>
			<HelpTip text="Use SMTP for generic email relay, Resend/Brevo for API delivery. Add test recipients above in the main panel to validate setup quickly." />
		</div>

		<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
			<label class="space-y-1">
				<span class="text-xs text-gray-500">Channel Name</span>
				<input class="ds-input-sm" placeholder="Ops SMTP" bind:value={channelForm.name} required />
			</label>
			<label class="space-y-1">
				<span class="text-xs text-gray-500">Channel Type</span>
				<Combobox
					options={alertChannelTypeOptions}
					value={channelForm.channel_type}
					onChange={(v) => channelForm = { ...channelForm, channel_type: v as AlertChannelType }}
				/>
			</label>
		</div>

		{#if channelForm.channel_type === 'smtp'}
			<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
				<label class="space-y-1">
					<span class="text-xs text-gray-500">SMTP Host</span>
					<input class="ds-input-sm" placeholder="smtp.sendgrid.net" bind:value={channelForm.smtp_host} required />
				</label>
				<label class="space-y-1">
					<span class="text-xs text-gray-500">SMTP Port</span>
					<input class="ds-input-sm" type="number" min="1" max="65535" bind:value={channelForm.smtp_port} />
				</label>
				<label class="space-y-1">
					<span class="text-xs text-gray-500">Username</span>
					<input class="ds-input-sm" bind:value={channelForm.smtp_username} />
				</label>
				<label class="space-y-1">
					<span class="text-xs text-gray-500">Password</span>
					<input class="ds-input-sm" type="password" bind:value={channelForm.smtp_password} />
				</label>
				<label class="space-y-1">
					<span class="text-xs text-gray-500">From Email</span>
					<input class="ds-input-sm" type="email" placeholder="alerts@company.com" bind:value={channelForm.smtp_from_email} required />
				</label>
				<label class="space-y-1">
					<span class="text-xs text-gray-500">From Name</span>
					<input class="ds-input-sm" placeholder="CH-UI Alerts" bind:value={channelForm.smtp_from_name} />
				</label>
			</div>
			<div class="flex flex-wrap items-center gap-4">
				<label class="ds-checkbox-label text-xs">
					<input type="checkbox" class="ds-checkbox" bind:checked={channelForm.smtp_use_tls} />
					TLS
				</label>
				<label class="ds-checkbox-label text-xs">
					<input type="checkbox" class="ds-checkbox" bind:checked={channelForm.smtp_starttls} />
					STARTTLS
				</label>
			</div>
		{:else}
			<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
				<label class="space-y-1 md:col-span-2">
					<span class="text-xs text-gray-500">API Key</span>
					<input class="ds-input-sm" type="password" bind:value={channelForm.api_key} required />
				</label>
				<label class="space-y-1">
					<span class="text-xs text-gray-500">From Email</span>
					<input class="ds-input-sm" type="email" placeholder="alerts@company.com" bind:value={channelForm.api_from_email} required />
				</label>
				<label class="space-y-1">
					<span class="text-xs text-gray-500">From Name</span>
					<input class="ds-input-sm" placeholder="CH-UI Alerts" bind:value={channelForm.api_from_name} />
				</label>
				<label class="space-y-1 md:col-span-2">
					<span class="text-xs text-gray-500">Base URL (optional)</span>
					<input class="ds-input-sm" placeholder="Leave empty for provider default" bind:value={channelForm.api_base_url} />
				</label>
			</div>
		{/if}

		<label class="ds-checkbox-label text-xs">
			<input type="checkbox" class="ds-checkbox" bind:checked={channelForm.is_active} />
			Active
		</label>

		<div class="flex items-center justify-end gap-2 pt-2 border-t border-gray-200 dark:border-gray-800">
			<button type="button" class="ds-btn-outline" onclick={() => channelSheetOpen = false}>Cancel</button>
			<button type="submit" class="ds-btn-primary" disabled={!channelForm.name.trim()}>Create Channel</button>
		</div>
	</form>
</Sheet>

<Sheet
	open={ruleSheetOpen}
	title="Create Alert Rule"
	size="xl"
	onclose={() => ruleSheetOpen = false}
>
	<form
		class="space-y-4"
		onsubmit={(e) => {
			e.preventDefault();
			void createAlertRuleRecord();
		}}
	>
		<div class="flex items-center gap-2">
			<p class="text-xs text-gray-500">Rules map governance/system events to delivery routes and escalation behavior.</p>
			<HelpTip text="Each route must include a channel and recipients. You can mix immediate and digest routes under the same rule." />
		</div>

		<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
			<label class="space-y-1">
				<span class="text-xs text-gray-500">Rule Name</span>
				<input class="ds-input-sm" placeholder="Critical policy violations" bind:value={ruleForm.name} required />
			</label>
			<label class="space-y-1">
				<span class="text-xs text-gray-500">Event Type</span>
				<Combobox
					options={alertEventTypeOptions}
					value={ruleForm.event_type}
					onChange={(v) => ruleForm = { ...ruleForm, event_type: v }}
				/>
			</label>
			<label class="space-y-1">
				<span class="text-xs text-gray-500">Minimum Severity</span>
				<Combobox
					options={alertSeverityOptions}
					value={ruleForm.severity_min}
					onChange={(v) => ruleForm = { ...ruleForm, severity_min: v }}
				/>
			</label>
			<label class="space-y-1">
				<span class="text-xs text-gray-500">Cooldown (seconds)</span>
				<input class="ds-input-sm" type="number" min="0" bind:value={ruleForm.cooldown_seconds} />
			</label>
			<label class="space-y-1">
				<span class="text-xs text-gray-500">Max Attempts</span>
				<input class="ds-input-sm" type="number" min="1" bind:value={ruleForm.max_attempts} />
			</label>
			<label class="space-y-1">
				<span class="text-xs text-gray-500">Subject Template</span>
				<input class="ds-input-sm" placeholder="Optional" bind:value={ruleForm.subject_template} />
			</label>
			<label class="space-y-1 md:col-span-2">
				<span class="text-xs text-gray-500">Body Template</span>
				<input class="ds-input-sm" placeholder="Optional" bind:value={ruleForm.body_template} />
			</label>
		</div>

		<label class="ds-checkbox-label text-xs">
			<input type="checkbox" class="ds-checkbox" bind:checked={ruleForm.enabled} />
			Enabled
		</label>

		<div class="space-y-2 border-t border-gray-200 dark:border-gray-800 pt-3">
			<div class="flex items-center justify-between">
				<h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">Routes</h3>
				<button type="button" class="ds-btn-outline" onclick={() => addRuleRouteDraft()}>Add Route</button>
			</div>

			{#if alertChannels.length === 0}
				<p class="text-xs text-amber-600 dark:text-amber-400">Create at least one alert channel before adding routes.</p>
			{/if}

			{#each ruleRoutesDraft as route, idx}
				<div class="ds-panel-muted p-3 space-y-3">
					<div class="flex items-center justify-between">
						<p class="text-xs font-semibold text-gray-700 dark:text-gray-300">Route {idx + 1}</p>
						{#if ruleRoutesDraft.length > 1}
							<button type="button" class="text-xs text-red-500 hover:text-red-700" onclick={() => removeRuleRouteDraft(idx)}>Remove</button>
						{/if}
					</div>
					<div class="grid grid-cols-1 md:grid-cols-2 gap-3">
						<label class="space-y-1">
							<span class="text-xs text-gray-500">Channel</span>
							<Combobox
								options={alertChannelOptions()}
								value={route.channel_id}
								onChange={(v) => updateRuleRouteDraft(idx, { channel_id: v })}
							/>
						</label>
						<label class="space-y-1">
							<span class="text-xs text-gray-500">Recipients (comma-separated)</span>
							<input
								class="ds-input-sm"
								placeholder="ops@company.com, data@company.com"
								value={route.recipients}
								oninput={(e) => updateRuleRouteDraft(idx, { recipients: (e.target as HTMLInputElement).value })}
							/>
						</label>
						<label class="space-y-1">
							<span class="text-xs text-gray-500">Delivery Mode</span>
							<Combobox
								options={routeDeliveryModeOptions}
								value={route.delivery_mode}
								onChange={(v) => updateRuleRouteDraft(idx, { delivery_mode: v as 'immediate' | 'digest' })}
							/>
						</label>
						<label class="space-y-1">
							<span class="text-xs text-gray-500">Digest Window (minutes)</span>
							<input
								class="ds-input-sm"
								type="number"
								min="1"
								disabled={route.delivery_mode !== 'digest'}
								value={route.digest_window_minutes}
								oninput={(e) => updateRuleRouteDraft(idx, { digest_window_minutes: Number((e.target as HTMLInputElement).value) || 15 })}
							/>
						</label>
						<label class="space-y-1">
							<span class="text-xs text-gray-500">Escalation Channel</span>
							<Combobox
								options={alertChannelOptions()}
								value={route.escalation_channel_id}
								onChange={(v) => updateRuleRouteDraft(idx, { escalation_channel_id: v })}
							/>
						</label>
						<label class="space-y-1">
							<span class="text-xs text-gray-500">Escalation Recipients</span>
							<input
								class="ds-input-sm"
								placeholder="manager@company.com"
								value={route.escalation_recipients}
								oninput={(e) => updateRuleRouteDraft(idx, { escalation_recipients: (e.target as HTMLInputElement).value })}
							/>
						</label>
						<label class="space-y-1">
							<span class="text-xs text-gray-500">Escalate After Failures</span>
							<input
								class="ds-input-sm"
								type="number"
								min="0"
								value={route.escalation_after_failures}
								oninput={(e) => updateRuleRouteDraft(idx, { escalation_after_failures: Number((e.target as HTMLInputElement).value) || 0 })}
							/>
						</label>
					</div>
					<label class="ds-checkbox-label text-xs">
						<input
							type="checkbox"
							class="ds-checkbox"
							checked={route.is_active}
							onchange={(e) => updateRuleRouteDraft(idx, { is_active: (e.target as HTMLInputElement).checked })}
						/>
						Route active
					</label>
				</div>
			{/each}
		</div>

		<div class="flex items-center justify-end gap-2 pt-2 border-t border-gray-200 dark:border-gray-800">
			<button type="button" class="ds-btn-outline" onclick={() => ruleSheetOpen = false}>Cancel</button>
			<button type="submit" class="ds-btn-primary" disabled={!ruleForm.name.trim()}>Create Rule</button>
		</div>
	</form>
</Sheet>
