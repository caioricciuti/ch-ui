<script lang="ts">
  import { onMount } from "svelte";
  import {
    getDatabases,
    isSchemaLoading,
    loadDatabases,
    loadTables,
    toggleDatabase,
    toggleTable,
  } from "../../stores/schema.svelte";
  import {
    openQueryTab,
    openTableTab,
    openDatabaseTab,
  } from "../../stores/tabs.svelte";
  import { getSession } from "../../stores/session.svelte";
  import { apiGet, apiPost, apiPostForm } from "../../api/client";
  import {
    success as toastSuccess,
    error as toastError,
  } from "../../stores/toast.svelte";
  import Spinner from "../common/Spinner.svelte";
  import Sheet from "../common/Sheet.svelte";
  import Combobox, { type ComboboxOption } from "../common/Combobox.svelte";
  import {
    Database,
    Table2,
    ChevronRight,
    ChevronDown,
    Columns3,
    Search,
    X,
    MoreHorizontal,
    Play,
    Info,
    RefreshCw,
    Copy,
    FolderPlus,
    TableProperties,
    Trash2,
    Plus,
    Upload,
    AlertTriangle,
  } from "lucide-svelte";
  import ContextMenu, {
    type ContextMenuItem,
  } from "../common/ContextMenu.svelte";

  interface Props {
    onSelectTable?: (database: string, table: string) => void;
    onSelectDatabase?: (database: string) => void;
  }

  interface TableColumnDraft {
    id: string;
    name: string;
    type: string;
    defaultExpression: string;
    comment: string;
  }

  interface UploadColumnDraft {
    id: string;
    name: string;
    type: string;
    sample: string;
  }

  let { onSelectTable, onSelectDatabase }: Props = $props();

  onMount(() => {
    loadDatabases();
  });

  const databases = $derived(getDatabases());
  const loading = $derived(isSchemaLoading());
  const session = $derived(getSession());
  const canManageSchema = $derived(session?.role === "admin");

  let searchTerm = $state("");
  let showSystemDbs = $state(false);
  const SYSTEM_DBS = ["system", "information_schema", "INFORMATION_SCHEMA"];

  const databaseEngineOptions: ComboboxOption[] = [
    { value: "Atomic", label: "Atomic", hint: "Default modern engine" },
    { value: "Ordinary", label: "Ordinary", hint: "Legacy engine" },
    { value: "Lazy", label: "Lazy", hint: "On-demand metadata load" },
    {
      value: "Replicated",
      label: "Replicated",
      hint: "Replicated database metadata",
    },
  ];

  const tableEngineOptions: ComboboxOption[] = [
    {
      value: "MergeTree",
      label: "MergeTree",
      hint: "General purpose OLAP engine",
    },
    {
      value: "ReplacingMergeTree",
      label: "ReplacingMergeTree",
      hint: "Dedup by replacing rows",
    },
    {
      value: "SummingMergeTree",
      label: "SummingMergeTree",
      hint: "Summation by key on merge",
    },
    {
      value: "AggregatingMergeTree",
      label: "AggregatingMergeTree",
      hint: "Aggregated states storage",
    },
    {
      value: "CollapsingMergeTree",
      label: "CollapsingMergeTree",
      hint: "Row collapsing with sign column",
    },
    {
      value: "VersionedCollapsingMergeTree",
      label: "VersionedCollapsingMergeTree",
      hint: "Version-aware collapse",
    },
    { value: "Log", label: "Log", hint: "Simple append-only log" },
    { value: "StripeLog", label: "StripeLog", hint: "Column stripe log" },
    { value: "TinyLog", label: "TinyLog", hint: "Lightweight tiny log" },
    { value: "Memory", label: "Memory", hint: "In-memory ephemeral table" },
  ];

  let columnSeed = 0;
  function newColumnDraft(name = "", type = "String"): TableColumnDraft {
    columnSeed += 1;
    return {
      id: `col-${columnSeed}`,
      name,
      type,
      defaultExpression: "",
      comment: "",
    };
  }

  type MenuTarget =
    | { kind: "root" }
    | { kind: "database"; database: string }
    | { kind: "table"; database: string; table: string };

  let menu = $state<{ target: MenuTarget; x: number; y: number } | null>(null);

  let clusters = $state<string[]>([]);
  let clustersLoading = $state(false);
  let dataTypes = $state<string[]>([]);
  let dataTypesLoading = $state(false);

  const fallbackDataTypes = [
    "String",
    "UInt8",
    "UInt16",
    "UInt32",
    "UInt64",
    "Int8",
    "Int16",
    "Int32",
    "Int64",
    "Float32",
    "Float64",
    "Boolean",
    "UUID",
    "Date",
    "Date32",
    "DateTime",
    "DateTime64(3)",
    "Decimal(18, 2)",
    "Array(String)",
    "Tuple(String, UInt64)",
    "Map(String, String)",
    "Nullable(String)",
    "LowCardinality(String)",
    "FixedString(16)",
    "IPv4",
    "IPv6",
    "JSON",
  ];

  const parametricTypeTemplates: Record<string, string> = {
    Array: "Array(String)",
    Tuple: "Tuple(String, UInt64)",
    Map: "Map(String, String)",
    Nullable: "Nullable(String)",
    LowCardinality: "LowCardinality(String)",
    Decimal: "Decimal(18, 2)",
    DateTime64: "DateTime64(3)",
    FixedString: "FixedString(16)",
    AggregateFunction: "AggregateFunction(sum, UInt64)",
    SimpleAggregateFunction: "SimpleAggregateFunction(sum, UInt64)",
    Enum8: "Enum8('a' = 1)",
    Enum16: "Enum16('a' = 1)",
  };

  function normalizeDataTypeTemplate(typeName: string): string {
    const trimmed = typeName.trim();
    if (!trimmed) return trimmed;
    return parametricTypeTemplates[trimmed] ?? trimmed;
  }

  let createDatabaseSheetOpen = $state(false);
  let createDatabaseSubmitting = $state(false);
  let createDatabaseForm = $state({
    name: "",
    engine: "Atomic",
    onCluster: "",
    ifNotExists: true,
  });

  let createTableSheetOpen = $state(false);
  let createTableSubmitting = $state(false);
  let createTableErrorMessage = $state("");
  let createTableForm = $state({
    database: "",
    name: "",
    engine: "MergeTree",
    onCluster: "",
    ifNotExists: true,
    orderBy: "",
    partitionBy: "",
    primaryKey: "",
    sampleBy: "",
    ttl: "",
    settings: "",
    comment: "",
    columns: [
      newColumnDraft("id", "UInt64"),
      newColumnDraft("created_at", "DateTime"),
    ],
  });

  let deleteDatabaseSheetOpen = $state(false);
  let deleteDatabaseSubmitting = $state(false);
  let deleteDatabaseForm = $state({
    name: "",
    onCluster: "",
    sync: true,
    typedName: "",
  });

  let deleteTableSheetOpen = $state(false);
  let deleteTableSubmitting = $state(false);
  let deleteTableForm = $state({
    database: "",
    name: "",
    onCluster: "",
    sync: true,
    typedName: "",
  });

  type UploadTargetMode = "new" | "existing";
  let uploadSheetOpen = $state(false);
  let uploadDiscovering = $state(false);
  let uploadSubmitting = $state(false);
  let uploadTablesLoading = $state(false);
  let uploadSourceFile = $state<File | null>(null);
  let uploadSourceFormat = $state("");
  let uploadRowsDetected = $state(0);
  let uploadPreviewRows = $state<Array<Record<string, unknown>>>([]);
  let uploadColumns = $state<UploadColumnDraft[]>([]);
  let uploadErrorMessage = $state("");
  let uploadCreateSQL = $state("");
  let uploadInsertSQL = $state("");
  let uploadForm = $state({
    database: "",
    mode: "new" as UploadTargetMode,
    tableName: "",
    existingTable: "",
    engine: "MergeTree",
    onCluster: "",
    ifNotExists: true,
    orderBy: "",
    partitionBy: "",
    primaryKey: "",
    comment: "",
  });
  let uploadTables = $state<string[]>([]);

  const uploadAccept = ".csv,.parquet,.json,.jsonl";
  let uploadColumnSeed = 0;

  const databaseOptions = $derived.by<ComboboxOption[]>(() =>
    databases
      .map((db) => ({
        value: db.name,
        label: db.name,
        hint: db.tables
          ? `${db.tables.length} tables loaded`
          : "Tables not loaded",
      }))
      .sort((a, b) => a.label.localeCompare(b.label)),
  );

  const clusterOptions = $derived.by<ComboboxOption[]>(() => [
    {
      value: "",
      label: "No cluster (local only)",
      hint: "Run on connected server only",
    },
    ...clusters.map((cluster) => ({ value: cluster, label: cluster })),
  ]);

  const dataTypeOptions = $derived.by<ComboboxOption[]>(() => {
    const source = dataTypes.length > 0 ? dataTypes : fallbackDataTypes;
    const options: ComboboxOption[] = [];
    const seen = new Set<string>();
    for (const typeName of source) {
      const template = normalizeDataTypeTemplate(typeName);
      if (!template || seen.has(template)) continue;
      seen.add(template);
      options.push({
        value: template,
        label: template,
        keywords: `${typeName.toLowerCase()} ${template.toLowerCase()}`,
      });
    }
    return options;
  });

  const uploadTableOptions = $derived.by<ComboboxOption[]>(() =>
    uploadTables.map((table) => ({
      value: table,
      label: table,
    })),
  );

  const filteredDatabases = $derived.by(() => {
    let dbs = databases;
    if (!showSystemDbs) {
      dbs = dbs.filter((db) => !SYSTEM_DBS.includes(db.name));
    }
    if (!searchTerm.trim()) return dbs;

    const term = searchTerm.toLowerCase();
    return dbs
      .map((db) => {
        const dbMatches = db.name.toLowerCase().includes(term);
        const matchingTables = db.tables?.filter((t) =>
          t.name.toLowerCase().includes(term),
        );
        if (dbMatches) return db;
        if (matchingTables && matchingTables.length > 0) {
          return { ...db, tables: matchingTables, expanded: true };
        }
        return null;
      })
      .filter(Boolean) as typeof dbs;
  });

  function isSystemDatabase(name: string): boolean {
    return SYSTEM_DBS.includes(name);
  }

  function closeMenu() {
    menu = null;
  }

  function openSchemaActionsMenu(e: MouseEvent) {
    e.preventDefault();
    e.stopPropagation();
    const target = e.currentTarget as HTMLElement | null;
    const rect = target?.getBoundingClientRect();
    menu = {
      target: { kind: "root" },
      x: rect ? rect.right - 180 : e.clientX,
      y: rect ? rect.bottom + 6 : e.clientY,
    };
  }

  function openContextMenu(e: MouseEvent, target: MenuTarget) {
    e.preventDefault();
    e.stopPropagation();
    menu = {
      target,
      x: Math.min(window.innerWidth - 220, e.clientX),
      y: Math.min(window.innerHeight - 220, e.clientY),
    };
  }

  function selectTable(database: string, table: string) {
    if (onSelectTable) {
      onSelectTable(database, table);
    } else {
      openTableTab(database, table);
    }
  }

  function selectDatabase(database: string) {
    if (onSelectDatabase) {
      onSelectDatabase(database);
    } else {
      openDatabaseTab(database);
    }
  }

  async function copyToClipboard(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      toastSuccess("Copied to clipboard");
    } catch {
      toastError("Clipboard unavailable");
    } finally {
      closeMenu();
    }
  }

  function queryDatabase(database: string) {
    openQueryTab(`SHOW TABLES FROM \`${database}\``);
    closeMenu();
  }

  function queryTable(database: string, table: string) {
    openQueryTab(`SELECT *\nFROM \`${database}\`.\`${table}\`\nLIMIT 1000`);
    closeMenu();
  }

  function viewDatabaseInfo(database: string) {
    selectDatabase(database);
    closeMenu();
  }

  function viewTableInfo(database: string, table: string) {
    selectTable(database, table);
    closeMenu();
  }

  function refreshDatabase(database: string) {
    loadTables(database);
    closeMenu();
  }

  async function refreshSchema() {
    if (loading) return;
    await loadDatabases();
  }

  async function loadClusters(force = false) {
    if (clustersLoading) return;
    if (!force && clusters.length > 0) return;
    clustersLoading = true;
    try {
      const res = await apiGet<{ clusters: string[] }>("/api/query/clusters");
      clusters = res.clusters ?? [];
    } catch {
      clusters = [];
    } finally {
      clustersLoading = false;
    }
  }

  async function loadDataTypes(force = false) {
    if (dataTypesLoading) return;
    if (!force && dataTypes.length > 0) return;
    dataTypesLoading = true;
    try {
      const res = await apiGet<{ data_types: string[] }>(
        "/api/query/data-types",
      );
      const incoming = res.data_types ?? [];
      const unique = Array.from(
        new Set(incoming.map((typeName) => typeName.trim()).filter(Boolean)),
      );
      dataTypes = unique.sort((a, b) => a.localeCompare(b));
    } catch {
      dataTypes = [];
    } finally {
      dataTypesLoading = false;
    }
  }

  function dataTypeOptionsFor(selectedType: string): ComboboxOption[] {
    const options = dataTypeOptions;
    const normalized = selectedType.trim();
    if (!normalized) return options;
    if (options.some((opt) => opt.value === normalized)) return options;
    return [{ value: normalized, label: normalized }, ...options];
  }

  function resetCreateDatabaseForm() {
    createDatabaseForm = {
      name: "",
      engine: "Atomic",
      onCluster: "",
      ifNotExists: true,
    };
  }

  function resetCreateTableForm() {
    createTableErrorMessage = "";
    createTableForm = {
      database: "",
      name: "",
      engine: "MergeTree",
      onCluster: "",
      ifNotExists: true,
      orderBy: "",
      partitionBy: "",
      primaryKey: "",
      sampleBy: "",
      ttl: "",
      settings: "",
      comment: "",
      columns: [
        newColumnDraft("id", "UInt64"),
        newColumnDraft("created_at", "DateTime"),
      ],
    };
  }

  function openCreateDatabaseSheet() {
    if (!canManageSchema) {
      toastError("Admin role required");
      return;
    }
    closeMenu();
    resetCreateDatabaseForm();
    createDatabaseSheetOpen = true;
    loadClusters();
  }

  function openCreateTableSheet(database = "") {
    if (!canManageSchema) {
      toastError("Admin role required");
      return;
    }
    closeMenu();
    resetCreateTableForm();
    createTableForm = { ...createTableForm, database };
    createTableSheetOpen = true;
    loadClusters();
    loadDataTypes();
  }

  function newUploadColumnDraft(
    name = "",
    type = "String",
    sample = "",
  ): UploadColumnDraft {
    uploadColumnSeed += 1;
    return {
      id: `upload-col-${uploadColumnSeed}`,
      name,
      type,
      sample,
    };
  }

  function resetUploadForm() {
    uploadSourceFile = null;
    uploadSourceFormat = "";
    uploadRowsDetected = 0;
    uploadPreviewRows = [];
    uploadColumns = [];
    uploadErrorMessage = "";
    uploadCreateSQL = "";
    uploadInsertSQL = "";
    uploadTables = [];
    uploadForm = {
      database: "",
      mode: "new",
      tableName: "",
      existingTable: "",
      engine: "MergeTree",
      onCluster: "",
      ifNotExists: true,
      orderBy: "",
      partitionBy: "",
      primaryKey: "",
      comment: "",
    };
  }

  async function loadUploadTables(database: string) {
    const dbName = database.trim();
    if (!dbName) {
      uploadTables = [];
      return;
    }
    uploadTablesLoading = true;
    try {
      const response = await apiGet<{ tables: string[] }>(
        `/api/query/tables?database=${encodeURIComponent(dbName)}`,
      );
      uploadTables = (response.tables ?? [])
        .slice()
        .sort((a, b) => a.localeCompare(b));
    } catch {
      uploadTables = [];
    } finally {
      uploadTablesLoading = false;
    }
  }

  function openUploadSheet(database = "") {
    if (!canManageSchema) {
      toastError("Admin role required");
      return;
    }
    closeMenu();
    resetUploadForm();
    uploadSheetOpen = true;
    uploadForm = { ...uploadForm, database: database.trim() };
    if (database.trim()) {
      loadUploadTables(database);
    }
    loadClusters();
    loadDataTypes();
  }

  function onUploadDatabaseChange(value: string) {
    uploadForm = {
      ...uploadForm,
      database: value,
      existingTable: "",
    };
    loadUploadTables(value);
  }

  function updateUploadColumn(id: string, patch: Partial<UploadColumnDraft>) {
    uploadColumns = uploadColumns.map((col) =>
      col.id === id ? { ...col, ...patch } : col,
    );
  }

  function onUploadFileSelected(event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0] ?? null;
    uploadSourceFile = file;
    uploadRowsDetected = 0;
    uploadSourceFormat = "";
    uploadPreviewRows = [];
    uploadColumns = [];
    uploadErrorMessage = "";
    uploadCreateSQL = "";
    uploadInsertSQL = "";
  }

  async function discoverUploadSchema() {
    if (!uploadSourceFile) {
      toastError("Select a file first");
      return;
    }

    uploadDiscovering = true;
    try {
      const formData = new FormData();
      formData.append("file", uploadSourceFile);
      const response = await apiPostForm<{
        format: string;
        rows: number;
        columns: Array<{ name: string; type: string; sample?: string }>;
        preview: Array<Record<string, unknown>>;
      }>("/api/query/upload/discover", formData);

      uploadSourceFormat = response.format ?? "";
      uploadRowsDetected = response.rows ?? 0;
      uploadPreviewRows = response.preview ?? [];
      uploadErrorMessage = "";
      uploadColumns = (response.columns ?? []).map((col) =>
        newUploadColumnDraft(
          col.name ?? "",
          col.type ?? "String",
          col.sample ?? "",
        ),
      );
      toastSuccess("Schema discovered");
    } catch (e: any) {
      toastError(e.message);
    } finally {
      uploadDiscovering = false;
    }
  }

  async function submitUpload() {
    if (!uploadSourceFile) {
      toastError("Select a file to upload");
      return;
    }

    const dbName = uploadForm.database.trim();
    if (!dbName) {
      toastError("Database is required");
      return;
    }

    const tableName = (
      uploadForm.mode === "new"
        ? uploadForm.tableName
        : uploadForm.existingTable
    ).trim();
    if (!tableName) {
      toastError(
        uploadForm.mode === "new"
          ? "Table name is required"
          : "Select a target table",
      );
      return;
    }

    if (uploadColumns.length === 0) {
      toastError("Run schema discovery before uploading");
      return;
    }

    const normalizedColumns = uploadColumns.map((col) => ({
      name: col.name.trim(),
      type: col.type.trim(),
    }));
    if (normalizedColumns.some((col) => !col.name || !col.type)) {
      toastError("Every discovered column requires name and type");
      return;
    }

    uploadSubmitting = true;
    try {
      const formData = new FormData();
      formData.append("file", uploadSourceFile);
      formData.append("database", dbName);
      formData.append("table", tableName);
      formData.append(
        "create_table",
        uploadForm.mode === "new" ? "true" : "false",
      );
      formData.append("columns", JSON.stringify(normalizedColumns));
      if (uploadForm.mode === "new") {
        formData.append("engine", uploadForm.engine);
        formData.append("on_cluster", uploadForm.onCluster || "");
        formData.append(
          "if_not_exists",
          uploadForm.ifNotExists ? "true" : "false",
        );
        formData.append("order_by", uploadForm.orderBy.trim());
        formData.append("partition_by", uploadForm.partitionBy.trim());
        formData.append("primary_key", uploadForm.primaryKey.trim());
        formData.append("comment", uploadForm.comment.trim());
      }

      const result = await apiPostForm<{
        rows_inserted: number;
        created_table: boolean;
        commands?: {
          create_table?: string;
          insert?: string;
        };
      }>("/api/query/upload/ingest", formData);

      uploadErrorMessage = "";
      uploadCreateSQL = result.commands?.create_table ?? "";
      uploadInsertSQL = result.commands?.insert ?? "";
      toastSuccess(
        `Uploaded ${result.rows_inserted ?? 0} rows into ${dbName}.${tableName}`,
      );
      uploadSheetOpen = false;
      await loadTables(dbName);
      selectTable(dbName, tableName);
    } catch (e: any) {
      uploadErrorMessage = e.message ?? "Upload failed";
      toastError("Upload failed. See details in the sheet.");
    } finally {
      uploadSubmitting = false;
    }
  }

  function openDeleteDatabaseSheet(database: string) {
    if (!canManageSchema) {
      toastError("Admin role required");
      return;
    }
    closeMenu();
    deleteDatabaseForm = {
      name: database,
      onCluster: "",
      sync: true,
      typedName: "",
    };
    deleteDatabaseSheetOpen = true;
    loadClusters();
  }

  function openDeleteTableSheet(database: string, table: string) {
    if (!canManageSchema) {
      toastError("Admin role required");
      return;
    }
    closeMenu();
    deleteTableForm = {
      database,
      name: table,
      onCluster: "",
      sync: true,
      typedName: "",
    };
    deleteTableSheetOpen = true;
    loadClusters();
  }

  function addTableColumn() {
    createTableForm = {
      ...createTableForm,
      columns: [...createTableForm.columns, newColumnDraft()],
    };
  }

  function removeTableColumn(id: string) {
    if (createTableForm.columns.length <= 1) return;
    createTableForm = {
      ...createTableForm,
      columns: createTableForm.columns.filter((col) => col.id !== id),
    };
  }

  function updateTableColumn(id: string, patch: Partial<TableColumnDraft>) {
    createTableForm = {
      ...createTableForm,
      columns: createTableForm.columns.map((col) =>
        col.id === id ? { ...col, ...patch } : col,
      ),
    };
  }

  async function submitCreateDatabase() {
    const name = createDatabaseForm.name.trim();
    if (!name) {
      toastError("Database name is required");
      return;
    }

    createDatabaseSubmitting = true;
    try {
      await apiPost("/api/query/schema/database", {
        name,
        engine: createDatabaseForm.engine,
        on_cluster: createDatabaseForm.onCluster || undefined,
        if_not_exists: createDatabaseForm.ifNotExists,
      });
      toastSuccess(`Database "${name}" created`);
      createDatabaseSheetOpen = false;
      await loadDatabases();
      await loadTables(name);
      selectDatabase(name);
    } catch (e: any) {
      toastError(e.message);
    } finally {
      createDatabaseSubmitting = false;
    }
  }

  async function submitCreateTable() {
    const dbName = createTableForm.database.trim();
    const tableName = createTableForm.name.trim();

    if (!dbName) {
      toastError("Database is required");
      return;
    }
    if (!tableName) {
      toastError("Table name is required");
      return;
    }

    const normalizedColumns = createTableForm.columns.map((col) => ({
      name: col.name.trim(),
      type: col.type.trim(),
      default_expression: col.defaultExpression.trim(),
      comment: col.comment.trim(),
    }));

    if (normalizedColumns.some((col) => !col.name || !col.type)) {
      toastError("Each column requires name and type");
      return;
    }

    createTableSubmitting = true;
    createTableErrorMessage = "";
    try {
      const result = await apiPost<{ command?: string }>(
        "/api/query/schema/table",
        {
          database: dbName,
          name: tableName,
          engine: createTableForm.engine,
          on_cluster: createTableForm.onCluster || undefined,
          if_not_exists: createTableForm.ifNotExists,
          columns: normalizedColumns,
          order_by: createTableForm.orderBy.trim(),
          partition_by: createTableForm.partitionBy.trim(),
          primary_key: createTableForm.primaryKey.trim(),
          sample_by: createTableForm.sampleBy.trim(),
          ttl: createTableForm.ttl.trim(),
          settings: createTableForm.settings.trim(),
          comment: createTableForm.comment.trim(),
        },
      );
      const command = result?.command?.trim();
      toastSuccess(`Table "${dbName}.${tableName}" created`);
      if (command) {
        toastSuccess("Command available in audit/details");
      }
      createTableSheetOpen = false;
      await loadTables(dbName);
      selectTable(dbName, tableName);
    } catch (e: any) {
      createTableErrorMessage = e.message ?? "Create table failed";
      toastError("Create table failed. See details in the sheet.");
    } finally {
      createTableSubmitting = false;
    }
  }

  function escapeIdentifierForPreview(input: string): string {
    return `\`${input.replace(/`/g, "``")}\``;
  }

  function escapeLiteralForPreview(input: string): string {
    return input.replace(/\\/g, "\\\\").replace(/'/g, "\\'");
  }

  function buildCreateTableCommandPreview(): string {
    const dbName = createTableForm.database.trim();
    const tableName = createTableForm.name.trim();
    const target =
      dbName && tableName
        ? `${escapeIdentifierForPreview(dbName)}.${escapeIdentifierForPreview(tableName)}`
        : "`database`.`table`";

    const parts: string[] = ["CREATE TABLE"];
    if (createTableForm.ifNotExists) parts.push("IF NOT EXISTS");
    parts.push(target);

    if (createTableForm.onCluster.trim()) {
      parts.push(
        `ON CLUSTER ${escapeIdentifierForPreview(createTableForm.onCluster.trim())}`,
      );
    }

    const normalizedColumns = createTableForm.columns
      .map((col) => ({
        name: col.name.trim(),
        type: col.type.trim(),
        defaultExpression: col.defaultExpression.trim(),
        comment: col.comment.trim(),
      }))
      .filter((col) => col.name && col.type);

    const columnLines = normalizedColumns.map((col) => {
      let line = `${escapeIdentifierForPreview(col.name)} ${col.type}`;
      if (col.defaultExpression) line += ` DEFAULT ${col.defaultExpression}`;
      if (col.comment)
        line += ` COMMENT '${escapeLiteralForPreview(col.comment)}'`;
      return line;
    });

    const lines: string[] = [];
    lines.push(`${parts.join(" ")} (`);
    lines.push(`  ${columnLines.join(",\n  ")}`);
    lines.push(")");
    lines.push(`ENGINE = ${createTableForm.engine.trim() || "MergeTree"}`);

    if (createTableForm.partitionBy.trim())
      lines.push(`PARTITION BY ${createTableForm.partitionBy.trim()}`);
    if (createTableForm.orderBy.trim())
      lines.push(`ORDER BY ${createTableForm.orderBy.trim()}`);
    if (createTableForm.primaryKey.trim())
      lines.push(`PRIMARY KEY ${createTableForm.primaryKey.trim()}`);
    if (createTableForm.sampleBy.trim())
      lines.push(`SAMPLE BY ${createTableForm.sampleBy.trim()}`);
    if (createTableForm.ttl.trim())
      lines.push(`TTL ${createTableForm.ttl.trim()}`);
    if (createTableForm.settings.trim())
      lines.push(`SETTINGS ${createTableForm.settings.trim()}`);
    if (createTableForm.comment.trim())
      lines.push(
        `COMMENT '${escapeLiteralForPreview(createTableForm.comment.trim())}'`,
      );

    return lines.join("\n");
  }

  async function submitDeleteDatabase() {
    if (deleteDatabaseForm.typedName.trim() !== deleteDatabaseForm.name) {
      toastError("Type the exact database name to confirm deletion");
      return;
    }

    deleteDatabaseSubmitting = true;
    try {
      await apiPost("/api/query/schema/database/drop", {
        name: deleteDatabaseForm.name,
        on_cluster: deleteDatabaseForm.onCluster || undefined,
        if_exists: true,
        sync: deleteDatabaseForm.sync,
      });
      toastSuccess(`Database "${deleteDatabaseForm.name}" deleted`);
      deleteDatabaseSheetOpen = false;
      await loadDatabases();
    } catch (e: any) {
      toastError(e.message);
    } finally {
      deleteDatabaseSubmitting = false;
    }
  }

  async function submitDeleteTable() {
    const qualified = `${deleteTableForm.database}.${deleteTableForm.name}`;
    if (deleteTableForm.typedName.trim() !== qualified) {
      toastError(`Type "${qualified}" to confirm deletion`);
      return;
    }

    deleteTableSubmitting = true;
    try {
      await apiPost("/api/query/schema/table/drop", {
        database: deleteTableForm.database,
        name: deleteTableForm.name,
        on_cluster: deleteTableForm.onCluster || undefined,
        if_exists: true,
        sync: deleteTableForm.sync,
      });
      toastSuccess(`Table "${qualified}" deleted`);
      deleteTableSheetOpen = false;
      await loadTables(deleteTableForm.database);
    } catch (e: any) {
      toastError(e.message);
    } finally {
      deleteTableSubmitting = false;
    }
  }

  function getExplorerMenuItems(): ContextMenuItem[] {
    if (!menu) return [];
    const target = menu.target;

    if (target.kind === "root") {
      return [
        {
          id: "root-create-db",
          label: "Create Database...",
          icon: FolderPlus,
          disabled: !canManageSchema,
          onSelect: () => openCreateDatabaseSheet(),
        },
        {
          id: "root-create-table",
          label: "Create Table...",
          icon: TableProperties,
          disabled: databases.length === 0 || !canManageSchema,
          onSelect: () => openCreateTableSheet(),
        },
        {
          id: "root-upload",
          label: "Upload Data...",
          icon: Upload,
          disabled: databases.length === 0 || !canManageSchema,
          onSelect: () => openUploadSheet(),
        },
        { id: "sep", separator: true },
        {
          id: "root-toggle-system",
          label: showSystemDbs
            ? "Hide System Databases"
            : "Show System Databases",
          icon: Database,
          onSelect: () => {
            showSystemDbs = !showSystemDbs;
            closeMenu();
          },
        },
        { id: "sep2", separator: true },
        {
          id: "root-refresh",
          label: "Refresh Schema",
          icon: RefreshCw,
          onSelect: async () => {
            closeMenu();
            await loadDatabases();
          },
        },
      ];
    }

    if (target.kind === "database") {
      const isSystem = isSystemDatabase(target.database);
      return [
        {
          id: "db-info",
          label: "View Database Info",
          icon: Info,
          shortcut: "Enter",
          onSelect: () => viewDatabaseInfo(target.database),
        },
        {
          id: "db-query",
          label: "Run Show Tables",
          icon: Play,
          shortcut: "Cmd/Ctrl+Enter",
          onSelect: () => queryDatabase(target.database),
        },
        {
          id: "db-refresh",
          label: "Refresh Tables",
          icon: RefreshCw,
          shortcut: "R",
          onSelect: () => refreshDatabase(target.database),
        },
        { id: "sep1", separator: true },
        {
          id: "db-create-table",
          label: "Create Table...",
          icon: TableProperties,
          disabled: isSystem || !canManageSchema,
          onSelect: () => openCreateTableSheet(target.database),
        },
        {
          id: "db-upload",
          label: "Upload Data...",
          icon: Upload,
          disabled: isSystem || !canManageSchema,
          onSelect: () => openUploadSheet(target.database),
        },
        {
          id: "db-delete",
          label: "Delete Database...",
          icon: Trash2,
          danger: true,
          disabled: isSystem || !canManageSchema,
          onSelect: () => openDeleteDatabaseSheet(target.database),
        },
        { id: "sep2", separator: true },
        {
          id: "db-copy",
          label: "Copy Database Name",
          icon: Copy,
          onSelect: () => copyToClipboard(target.database),
        },
      ];
    }

    return [
      {
        id: "table-info",
        label: "View Table Info",
        icon: Info,
        shortcut: "Enter",
        onSelect: () => viewTableInfo(target.database, target.table),
      },
      {
        id: "table-query",
        label: "Query Table",
        icon: Play,
        shortcut: "Cmd/Ctrl+Enter",
        onSelect: () => queryTable(target.database, target.table),
      },
      { id: "sep1", separator: true },
      {
        id: "table-delete",
        label: "Delete Table...",
        icon: Trash2,
        danger: true,
        disabled: isSystemDatabase(target.database) || !canManageSchema,
        onSelect: () => openDeleteTableSheet(target.database, target.table),
      },
      { id: "sep2", separator: true },
      {
        id: "table-copy",
        label: "Copy Qualified Name",
        icon: Copy,
        onSelect: () =>
          copyToClipboard(`\`${target.database}\`.\`${target.table}\``),
      },
    ];
  }
</script>

<svelte:window onkeydown={(e) => e.key === "Escape" && closeMenu()} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="flex flex-col h-full text-[13px]"
  oncontextmenu={(e) => openContextMenu(e, { kind: "root" })}
>
  <div class="px-2.5 py-2 border-b border-gray-200 dark:border-gray-800">
    <div class="flex items-center gap-1.5">
      <div
        class="flex items-center gap-1.5 px-2.5 py-1.5 bg-gray-200/60 dark:bg-gray-800/60 rounded-md border border-gray-300/50 dark:border-gray-700/50 focus-within:border-gray-400 dark:focus-within:border-gray-600 flex-1"
      >
        <Search size={13} class="text-gray-500 shrink-0" />
        <input
          type="text"
          placeholder="Filter databases and tables..."
          class="flex-1 bg-transparent text-[13px] text-gray-700 dark:text-gray-300 placeholder-gray-400 dark:placeholder-gray-600 outline-none"
          bind:value={searchTerm}
        />
        {#if searchTerm}
          <button
            class="text-gray-500 hover:text-gray-700 dark:hover:text-gray-300"
            onclick={() => (searchTerm = "")}
          >
            <X size={13} />
          </button>
        {/if}
      </div>

      <button
        class="ds-btn-outline h-[34px] px-2 shrink-0"
        onclick={openSchemaActionsMenu}
        title="Schema actions"
      >
        <MoreHorizontal size={14} />
      </button>
      <button
        class="ds-btn-outline h-[34px] px-2 shrink-0"
        onclick={refreshSchema}
        title="Refresh schema"
        disabled={loading}
      >
        <RefreshCw size={14} class={loading ? "animate-spin" : ""} />
      </button>
    </div>

    {#if !canManageSchema}
      <p class="mt-1 px-0.5 text-[10px] text-gray-500">
        Schema create/delete actions require admin role
      </p>
    {/if}
  </div>

  <div class="flex-1 overflow-auto">
    {#if loading && databases.length === 0}
      <div class="flex items-center justify-center py-8">
        <Spinner size="sm" />
      </div>
    {:else if filteredDatabases.length === 0}
      <div
        class="flex items-center justify-center py-8 text-xs text-gray-400 dark:text-gray-600"
      >
        {searchTerm ? "No matches" : "No databases"}
      </div>
    {:else}
      {#each filteredDatabases as db}
        <div>
          <div
            class="group flex items-center gap-1.5 w-full px-2.5 py-1.5 text-left hover:bg-gray-200/50 dark:hover:bg-gray-800/50 text-gray-700 dark:text-gray-300"
            oncontextmenu={(e) =>
              openContextMenu(e, { kind: "database", database: db.name })}
          >
            <button
              class="shrink-0 p-0.5"
              onclick={() => toggleDatabase(db.name)}
            >
              {#if db.expanded}
                <ChevronDown size={15} class="text-gray-500 shrink-0" />
              {:else}
                <ChevronRight size={15} class="text-gray-500 shrink-0" />
              {/if}
            </button>
            <button
              class="flex items-center gap-1.5 flex-1 min-w-0 text-left"
              onclick={() => toggleDatabase(db.name)}
            >
              <Database size={15} class="text-ch-blue shrink-0" />
              <span class="truncate">{db.name}</span>
              {#if db.loading}
                <Spinner size="sm" class="ml-auto" />
              {/if}
            </button>
            <button
              class="shrink-0 p-0.5 rounded text-gray-400 dark:text-gray-600 hover:text-gray-700 dark:hover:text-gray-300 opacity-0 group-hover:opacity-100 transition-opacity"
              onclick={(e) =>
                openContextMenu(e, { kind: "database", database: db.name })}
            >
              <MoreHorizontal size={15} />
            </button>
          </div>

          {#if db.expanded && db.tables}
            {#each db.tables as table}
              <div>
                <div
                  class="group flex items-center w-full pl-7 pr-1.5 py-1.5 text-gray-500 dark:text-gray-400 hover:bg-gray-200/50 dark:hover:bg-gray-800/50"
                  oncontextmenu={(e) =>
                    openContextMenu(e, {
                      kind: "table",
                      database: db.name,
                      table: table.name,
                    })}
                >
                  <button
                    class="shrink-0 p-0.5"
                    onclick={() => toggleTable(db.name, table.name)}
                  >
                    {#if table.expanded}
                      <ChevronDown
                        size={13}
                        class="text-gray-400 dark:text-gray-600"
                      />
                    {:else}
                      <ChevronRight
                        size={13}
                        class="text-gray-400 dark:text-gray-600"
                      />
                    {/if}
                  </button>
                  <button
                    class="flex items-center gap-1.5 flex-1 min-w-0 text-left"
                    onclick={() => selectTable(db.name, table.name)}
                  >
                    <Table2 size={14} class="text-gray-500 shrink-0" />
                    <span class="truncate">{table.name}</span>
                    {#if table.loading}
                      <Spinner size="sm" class="ml-auto" />
                    {/if}
                  </button>

                  <button
                    class="shrink-0 p-0.5 rounded text-gray-400 dark:text-gray-600 hover:text-gray-700 dark:hover:text-gray-300 opacity-0 group-hover:opacity-100 transition-opacity"
                    onclick={(e) =>
                      openContextMenu(e, {
                        kind: "table",
                        database: db.name,
                        table: table.name,
                      })}
                  >
                    <MoreHorizontal size={15} />
                  </button>
                </div>

                {#if table.expanded && table.columns}
                  {#each table.columns as col}
                    <div
                      class="flex items-center gap-1.5 pl-12 pr-2 py-0.5 text-[11px] text-gray-500"
                    >
                      <Columns3 size={12} class="shrink-0" />
                      <span class="truncate">{col.name}</span>
                      <span
                        class="ml-auto text-gray-400 dark:text-gray-600 truncate"
                        >{col.type}</span
                      >
                    </div>
                  {/each}
                {/if}
              </div>
            {/each}
          {/if}
        </div>
      {/each}
    {/if}
  </div>
</div>

<ContextMenu
  open={!!menu}
  x={menu?.x ?? 0}
  y={menu?.y ?? 0}
  items={getExplorerMenuItems()}
  onclose={closeMenu}
/>

<Sheet
  open={createDatabaseSheetOpen}
  title="Create Database"
  size="md"
  onclose={() => (createDatabaseSheetOpen = false)}
>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault();
      submitCreateDatabase();
    }}
  >
    <div class="grid gap-4 md:grid-cols-2">
      <div>
        <label class="ds-form-label">Database Name</label>
        <input
          class="ds-input"
          placeholder="analytics"
          bind:value={createDatabaseForm.name}
        />
      </div>
      <div>
        <label class="ds-form-label">Database Engine</label>
        <Combobox
          options={databaseEngineOptions}
          value={createDatabaseForm.engine}
          onChange={(value) =>
            (createDatabaseForm = { ...createDatabaseForm, engine: value })}
          placeholder="Select engine"
        />
      </div>
    </div>

    <div class="grid gap-4 md:grid-cols-2">
      <div>
        <label class="ds-form-label">Cluster (optional)</label>
        <Combobox
          options={clusterOptions}
          value={createDatabaseForm.onCluster}
          onChange={(value) =>
            (createDatabaseForm = { ...createDatabaseForm, onCluster: value })}
          placeholder={clustersLoading ? "Loading clusters..." : "No cluster"}
          disabled={clustersLoading}
        />
      </div>
      <div class="flex items-end pb-2">
        <label class="ds-checkbox-label">
          <input
            type="checkbox"
            class="ds-checkbox"
            bind:checked={createDatabaseForm.ifNotExists}
          />
          IF NOT EXISTS
        </label>
      </div>
    </div>

    <div class="ds-panel-muted p-3 text-xs text-gray-600 dark:text-gray-300">
      Creates a database with selected engine. If cluster is set, operation runs
      with <code>ON CLUSTER</code>.
    </div>

    <div class="flex items-center justify-end gap-2 pt-1">
      <button
        type="button"
        class="ds-btn-outline"
        onclick={() => (createDatabaseSheetOpen = false)}>Cancel</button
      >
      <button
        type="submit"
        class="ds-btn-primary"
        disabled={createDatabaseSubmitting}
      >
        <FolderPlus size={14} />
        {createDatabaseSubmitting ? "Creating..." : "Create Database"}
      </button>
    </div>
  </form>
</Sheet>

<Sheet
  open={createTableSheetOpen}
  title="Create Table"
  size="xl"
  onclose={() => (createTableSheetOpen = false)}
>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault();
      submitCreateTable();
    }}
  >
    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
      <div>
        <label class="ds-form-label">Database</label>
        <Combobox
          options={databaseOptions}
          value={createTableForm.database}
          onChange={(value) =>
            (createTableForm = { ...createTableForm, database: value })}
          placeholder="Select database"
        />
      </div>
      <div>
        <label class="ds-form-label">Table Name</label>
        <input
          class="ds-input"
          placeholder="events"
          bind:value={createTableForm.name}
        />
      </div>
      <div>
        <label class="ds-form-label">Engine</label>
        <Combobox
          options={tableEngineOptions}
          value={createTableForm.engine}
          onChange={(value) =>
            (createTableForm = { ...createTableForm, engine: value })}
          placeholder="Select engine"
        />
      </div>
      <div>
        <label class="ds-form-label">Cluster (optional)</label>
        <Combobox
          options={clusterOptions}
          value={createTableForm.onCluster}
          onChange={(value) =>
            (createTableForm = { ...createTableForm, onCluster: value })}
          placeholder={clustersLoading ? "Loading clusters..." : "No cluster"}
          disabled={clustersLoading}
        />
      </div>
    </div>

    <div class="ds-panel p-3 space-y-3">
      <div class="flex items-center justify-between gap-2">
        <div>
          <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">
            Columns
          </h3>
          <p class="text-[11px] text-gray-500">
            Type is selected from ClickHouse data types (parametric families are
            prefilled with valid templates).
          </p>
        </div>
        <button type="button" class="ds-btn-outline" onclick={addTableColumn}>
          <Plus size={13} />
          Add Column
        </button>
      </div>
      <div class="overflow-x-auto overflow-y-visible pb-1">
        <table class="ds-table min-w-[760px]">
          <thead>
            <tr class="ds-table-head-row">
              <th class="ds-table-th">Name</th>
              <th class="ds-table-th">Type</th>
              <th class="ds-table-th">Default Expression</th>
              <th class="ds-table-th">Comment</th>
              <th class="ds-table-th-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#each createTableForm.columns as col}
              <tr class="ds-table-row-static">
                <td class="py-2 px-3 align-top">
                  <input
                    class="ds-input-sm"
                    value={col.name}
                    oninput={(e) =>
                      updateTableColumn(col.id, {
                        name: (e.currentTarget as HTMLInputElement).value,
                      })}
                    placeholder="column_name"
                  />
                </td>
                <td class="py-2 px-3 align-top">
                  <Combobox
                    options={dataTypeOptionsFor(col.type)}
                    value={col.type}
                    onChange={(value) =>
                      updateTableColumn(col.id, { type: value })}
                    placeholder={dataTypesLoading
                      ? "Loading types..."
                      : "Select type"}
                    disabled={dataTypesLoading}
                  />
                </td>
                <td class="py-2 px-3 align-top">
                  <input
                    class="ds-input-sm"
                    value={col.defaultExpression}
                    oninput={(e) =>
                      updateTableColumn(col.id, {
                        defaultExpression: (e.currentTarget as HTMLInputElement)
                          .value,
                      })}
                    placeholder="now()"
                  />
                </td>
                <td class="py-2 px-3 align-top">
                  <input
                    class="ds-input-sm"
                    value={col.comment}
                    oninput={(e) =>
                      updateTableColumn(col.id, {
                        comment: (e.currentTarget as HTMLInputElement).value,
                      })}
                    placeholder="Business meaning"
                  />
                </td>
                <td class="py-2 px-3 align-top text-right">
                  <button
                    type="button"
                    class="ds-btn-ghost text-red-500 hover:text-red-600"
                    onclick={() => removeTableColumn(col.id)}
                    disabled={createTableForm.columns.length <= 1}
                  >
                    <Trash2 size={13} />
                  </button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>

    <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
      <div>
        <label class="ds-form-label">ORDER BY</label>
        <input
          class="ds-input"
          placeholder="tuple()"
          bind:value={createTableForm.orderBy}
        />
      </div>
      <div>
        <label class="ds-form-label">PARTITION BY</label>
        <input
          class="ds-input"
          placeholder="toYYYYMM(created_at)"
          bind:value={createTableForm.partitionBy}
        />
      </div>
      <div>
        <label class="ds-form-label">PRIMARY KEY</label>
        <input
          class="ds-input"
          placeholder="id"
          bind:value={createTableForm.primaryKey}
        />
      </div>
      <div>
        <label class="ds-form-label">SAMPLE BY</label>
        <input
          class="ds-input"
          placeholder="cityHash64(id)"
          bind:value={createTableForm.sampleBy}
        />
      </div>
      <div>
        <label class="ds-form-label">TTL</label>
        <input
          class="ds-input"
          placeholder="created_at + INTERVAL 90 DAY"
          bind:value={createTableForm.ttl}
        />
      </div>
      <div>
        <label class="ds-form-label">SETTINGS</label>
        <input
          class="ds-input"
          placeholder="index_granularity = 8192"
          bind:value={createTableForm.settings}
        />
      </div>
    </div>

    <div class="grid gap-4 md:grid-cols-2">
      <div>
        <label class="ds-form-label">Table Comment (optional)</label>
        <input
          class="ds-input"
          placeholder="Fact table for product analytics"
          bind:value={createTableForm.comment}
        />
      </div>
      <div class="flex items-end pb-2">
        <label class="ds-checkbox-label">
          <input
            type="checkbox"
            class="ds-checkbox"
            bind:checked={createTableForm.ifNotExists}
          />
          IF NOT EXISTS
        </label>
      </div>
    </div>

    <div class="ds-panel-muted p-3 space-y-1">
      <div class="text-xs font-semibold text-gray-700 dark:text-gray-200">
        Command Preview
      </div>
      <pre
        class="text-[11px] max-h-36 overflow-auto whitespace-pre-wrap break-all text-gray-600 dark:text-gray-300">{buildCreateTableCommandPreview()}</pre>
    </div>

    {#if createTableErrorMessage}
      <div class="rounded-lg border border-red-500/40 bg-red-500/10 p-3">
        <div class="text-xs font-semibold text-red-200 mb-1">
          Create Table Error
        </div>
        <pre
          class="text-[11px] whitespace-pre-wrap break-words max-h-36 overflow-auto text-red-100">{createTableErrorMessage}</pre>
      </div>
    {/if}

    <div class="flex items-center justify-end gap-2 pt-1">
      <button
        type="button"
        class="ds-btn-outline"
        onclick={() => (createTableSheetOpen = false)}>Cancel</button
      >
      <button
        type="submit"
        class="ds-btn-primary"
        disabled={createTableSubmitting}
      >
        <TableProperties size={14} />
        {createTableSubmitting ? "Creating..." : "Create Table"}
      </button>
    </div>
  </form>
</Sheet>

<Sheet
  open={deleteDatabaseSheetOpen}
  title="Delete Database"
  size="md"
  onclose={() => (deleteDatabaseSheetOpen = false)}
>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault();
      submitDeleteDatabase();
    }}
  >
    <div
      class="rounded-lg border border-red-500/40 bg-red-500/10 p-3 text-sm text-red-200 flex items-start gap-2"
    >
      <AlertTriangle size={16} class="mt-0.5 text-red-400" />
      <div>
        This will permanently delete <strong>{deleteDatabaseForm.name}</strong> and
        all tables inside it.
      </div>
    </div>

    <div class="grid gap-4 md:grid-cols-2">
      <div>
        <label class="ds-form-label">Cluster (optional)</label>
        <Combobox
          options={clusterOptions}
          value={deleteDatabaseForm.onCluster}
          onChange={(value) =>
            (deleteDatabaseForm = { ...deleteDatabaseForm, onCluster: value })}
          placeholder={clustersLoading ? "Loading clusters..." : "No cluster"}
          disabled={clustersLoading}
        />
      </div>
      <div class="flex items-end pb-2">
        <label class="ds-checkbox-label">
          <input
            type="checkbox"
            class="ds-checkbox"
            bind:checked={deleteDatabaseForm.sync}
          />
          Use SYNC drop
        </label>
      </div>
    </div>

    <div>
      <label class="ds-form-label">Type database name to confirm</label>
      <input
        class="ds-input"
        bind:value={deleteDatabaseForm.typedName}
        placeholder={deleteDatabaseForm.name}
      />
    </div>

    <div class="flex items-center justify-end gap-2 pt-1">
      <button
        type="button"
        class="ds-btn-outline"
        onclick={() => (deleteDatabaseSheetOpen = false)}>Cancel</button
      >
      <button
        type="submit"
        class="inline-flex items-center justify-center gap-1.5 rounded px-3 py-1.5 text-[13px] font-medium text-white bg-red-600 border border-red-500 transition-colors hover:bg-red-700 disabled:opacity-60 disabled:cursor-not-allowed"
        disabled={deleteDatabaseSubmitting ||
          deleteDatabaseForm.typedName.trim() !== deleteDatabaseForm.name}
      >
        <Trash2 size={14} />
        {deleteDatabaseSubmitting ? "Deleting..." : "Delete Database"}
      </button>
    </div>
  </form>
</Sheet>

<Sheet
  open={uploadSheetOpen}
  title="Upload Data"
  size="xl"
  onclose={() => (uploadSheetOpen = false)}
>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault();
      submitUpload();
    }}
  >
    <div class="ds-panel p-3 space-y-3">
      <div class="flex items-start justify-between gap-3">
        <div>
          <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">
            Source File
          </h3>
          <p class="text-[11px] text-gray-500">
            Accepted formats: CSV, Parquet, JSON, JSONL.
          </p>
        </div>
        <button
          type="button"
          class="ds-btn-outline"
          onclick={discoverUploadSchema}
          disabled={!uploadSourceFile || uploadDiscovering}
        >
          <Upload size={13} />
          {uploadDiscovering ? "Discovering..." : "Discover Schema"}
        </button>
      </div>

      <input
        type="file"
        class="ds-input"
        accept={uploadAccept}
        onchange={onUploadFileSelected}
      />

      <div class="grid gap-3 md:grid-cols-3">
        <div class="ds-panel-muted p-2.5">
          <div class="text-[10px] uppercase tracking-wide text-gray-500">
            Rows detected
          </div>
          <div
            class="mt-0.5 text-sm font-semibold text-gray-800 dark:text-gray-200"
          >
            {uploadRowsDetected || "—"}
          </div>
        </div>
        <div class="ds-panel-muted p-2.5">
          <div class="text-[10px] uppercase tracking-wide text-gray-500">
            Format
          </div>
          <div
            class="mt-0.5 text-sm font-semibold text-gray-800 dark:text-gray-200"
          >
            {uploadSourceFormat || "—"}
          </div>
        </div>
        <div class="ds-panel-muted p-2.5">
          <div class="text-[10px] uppercase tracking-wide text-gray-500">
            Columns
          </div>
          <div
            class="mt-0.5 text-sm font-semibold text-gray-800 dark:text-gray-200"
          >
            {uploadColumns.length || "—"}
          </div>
        </div>
      </div>
    </div>

    <div class="ds-panel p-3 space-y-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">
          Target
        </h3>
        <div
          class="inline-flex rounded-lg border border-gray-300 dark:border-gray-700 overflow-hidden"
        >
          <button
            type="button"
            class="px-3 py-1.5 text-xs {uploadForm.mode === 'new'
              ? 'bg-ch-blue/20 text-ch-blue'
              : 'text-gray-500 hover:bg-gray-200/55 dark:hover:bg-gray-800/55'}"
            onclick={() => (uploadForm = { ...uploadForm, mode: "new" })}
          >
            Create New Table
          </button>
          <button
            type="button"
            class="px-3 py-1.5 text-xs border-l border-gray-300 dark:border-gray-700 {uploadForm.mode ===
            'existing'
              ? 'bg-ch-blue/20 text-ch-blue'
              : 'text-gray-500 hover:bg-gray-200/55 dark:hover:bg-gray-800/55'}"
            onclick={() => (uploadForm = { ...uploadForm, mode: "existing" })}
          >
            Existing Table
          </button>
        </div>
      </div>

      <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <div>
          <label class="ds-form-label">Database</label>
          <Combobox
            options={databaseOptions}
            value={uploadForm.database}
            onChange={onUploadDatabaseChange}
            placeholder="Select database"
          />
        </div>

        {#if uploadForm.mode === "new"}
          <div>
            <label class="ds-form-label">Table Name</label>
            <input
              class="ds-input"
              bind:value={uploadForm.tableName}
              placeholder="events_upload"
            />
          </div>
          <div>
            <label class="ds-form-label">Engine</label>
            <Combobox
              options={tableEngineOptions}
              value={uploadForm.engine}
              onChange={(value) =>
                (uploadForm = { ...uploadForm, engine: value })}
              placeholder="Select engine"
            />
          </div>
          <div>
            <label class="ds-form-label">Cluster (optional)</label>
            <Combobox
              options={clusterOptions}
              value={uploadForm.onCluster}
              onChange={(value) =>
                (uploadForm = { ...uploadForm, onCluster: value })}
              placeholder={clustersLoading
                ? "Loading clusters..."
                : "No cluster"}
              disabled={clustersLoading}
            />
          </div>
        {:else}
          <div class="md:col-span-3">
            <label class="ds-form-label">Target Table</label>
            <Combobox
              options={uploadTableOptions}
              value={uploadForm.existingTable}
              onChange={(value) =>
                (uploadForm = { ...uploadForm, existingTable: value })}
              placeholder={uploadTablesLoading
                ? "Loading tables..."
                : "Select table"}
              disabled={uploadTablesLoading || !uploadForm.database}
            />
          </div>
        {/if}
      </div>

      {#if uploadForm.mode === "new"}
        <div class="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          <div>
            <label class="ds-form-label">ORDER BY</label>
            <input
              class="ds-input"
              bind:value={uploadForm.orderBy}
              placeholder="tuple()"
            />
          </div>
          <div>
            <label class="ds-form-label">PARTITION BY</label>
            <input
              class="ds-input"
              bind:value={uploadForm.partitionBy}
              placeholder="toYYYYMM(created_at)"
            />
          </div>
          <div>
            <label class="ds-form-label">PRIMARY KEY</label>
            <input
              class="ds-input"
              bind:value={uploadForm.primaryKey}
              placeholder="id"
            />
          </div>
          <div class="flex items-end pb-2">
            <label class="ds-checkbox-label">
              <input
                type="checkbox"
                class="ds-checkbox"
                bind:checked={uploadForm.ifNotExists}
              />
              IF NOT EXISTS
            </label>
          </div>
          <div class="md:col-span-2 lg:col-span-4">
            <label class="ds-form-label">Table Comment (optional)</label>
            <input
              class="ds-input"
              bind:value={uploadForm.comment}
              placeholder="Uploaded dataset table"
            />
          </div>
        </div>
      {/if}
    </div>

    <div class="ds-panel p-3 space-y-3">
      <div class="flex items-center justify-between gap-2">
        <h3 class="text-sm font-semibold text-gray-800 dark:text-gray-200">
          Discovered Columns
        </h3>
        <span class="text-[11px] text-gray-500"
          >Edit inferred types before upload if needed.</span
        >
      </div>

      {#if uploadColumns.length === 0}
        <div class="text-xs text-gray-500">
          Run "Discover Schema" to populate columns.
        </div>
      {:else}
        <div class="overflow-x-auto overflow-y-visible pb-1">
          <table class="ds-table min-w-[720px]">
            <thead>
              <tr class="ds-table-head-row">
                <th class="ds-table-th">Name</th>
                <th class="ds-table-th">Type</th>
                <th class="ds-table-th">Sample</th>
              </tr>
            </thead>
            <tbody>
              {#each uploadColumns as col}
                <tr class="ds-table-row-static">
                  <td class="py-2 px-3 align-top">
                    <input
                      class="ds-input-sm"
                      value={col.name}
                      oninput={(e) =>
                        updateUploadColumn(col.id, {
                          name: (e.currentTarget as HTMLInputElement).value,
                        })}
                    />
                  </td>
                  <td class="py-2 px-3 align-top">
                    <Combobox
                      options={dataTypeOptionsFor(col.type)}
                      value={col.type}
                      onChange={(value) =>
                        updateUploadColumn(col.id, { type: value })}
                      placeholder={dataTypesLoading
                        ? "Loading types..."
                        : "Select type"}
                      disabled={dataTypesLoading}
                    />
                  </td>
                  <td
                    class="py-2 px-3 align-top text-xs text-gray-500 dark:text-gray-400"
                  >
                    <span class="line-clamp-2 break-all"
                      >{col.sample || "—"}</span
                    >
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div>

    <div class="ds-panel-muted p-3">
      <div class="text-xs font-medium text-gray-700 dark:text-gray-200 mb-1">
        Preview
      </div>
      {#if uploadPreviewRows.length === 0}
        <div class="text-xs text-gray-500">No preview rows yet.</div>
      {:else}
        <pre
          class="text-[11px] max-h-40 overflow-auto whitespace-pre-wrap break-all text-gray-600 dark:text-gray-300">{JSON.stringify(
            uploadPreviewRows.slice(0, 5),
            null,
            2,
          )}</pre>
      {/if}
    </div>

    {#if uploadErrorMessage}
      <div class="rounded-lg border border-red-500/40 bg-red-500/10 p-3">
        <div class="text-xs font-semibold text-red-200 mb-1">Upload Error</div>
        <pre
          class="text-[11px] whitespace-pre-wrap break-words max-h-36 overflow-auto text-red-100">{uploadErrorMessage}</pre>
      </div>
    {/if}

    {#if uploadCreateSQL || uploadInsertSQL}
      <div class="ds-panel p-3 space-y-2">
        <div class="text-xs font-semibold text-gray-800 dark:text-gray-200">
          Executed Commands
        </div>
        {#if uploadCreateSQL}
          <div>
            <div class="text-[11px] text-gray-500 mb-1">CREATE TABLE</div>
            <pre
              class="text-[11px] max-h-32 overflow-auto whitespace-pre-wrap break-all text-gray-600 dark:text-gray-300">{uploadCreateSQL}</pre>
          </div>
        {/if}
        {#if uploadInsertSQL}
          <div>
            <div class="text-[11px] text-gray-500 mb-1">
              INSERT (sample/batch)
            </div>
            <pre
              class="text-[11px] max-h-32 overflow-auto whitespace-pre-wrap break-all text-gray-600 dark:text-gray-300">{uploadInsertSQL}</pre>
          </div>
        {/if}
      </div>
    {/if}

    <div class="flex items-center justify-end gap-2 pt-1">
      <button
        type="button"
        class="ds-btn-outline"
        onclick={() => (uploadSheetOpen = false)}>Cancel</button
      >
      <button
        type="submit"
        class="ds-btn-primary"
        disabled={uploadSubmitting || uploadDiscovering}
      >
        <Upload size={14} />
        {uploadSubmitting ? "Uploading..." : "Upload Data"}
      </button>
    </div>
  </form>
</Sheet>

<Sheet
  open={deleteTableSheetOpen}
  title="Delete Table"
  size="md"
  onclose={() => (deleteTableSheetOpen = false)}
>
  <form
    class="space-y-4"
    onsubmit={(e) => {
      e.preventDefault();
      submitDeleteTable();
    }}
  >
    <div
      class="rounded-lg border border-red-500/40 bg-red-500/10 p-3 text-sm text-red-200 flex items-start gap-2"
    >
      <AlertTriangle size={16} class="mt-0.5 text-red-400" />
      <div>
        This will permanently delete <strong
          >{deleteTableForm.database}.{deleteTableForm.name}</strong
        >.
      </div>
    </div>

    <div class="grid gap-4 md:grid-cols-2">
      <div>
        <label class="ds-form-label">Cluster (optional)</label>
        <Combobox
          options={clusterOptions}
          value={deleteTableForm.onCluster}
          onChange={(value) =>
            (deleteTableForm = { ...deleteTableForm, onCluster: value })}
          placeholder={clustersLoading ? "Loading clusters..." : "No cluster"}
          disabled={clustersLoading}
        />
      </div>
      <div class="flex items-end pb-2">
        <label class="ds-checkbox-label">
          <input
            type="checkbox"
            class="ds-checkbox"
            bind:checked={deleteTableForm.sync}
          />
          Use SYNC drop
        </label>
      </div>
    </div>

    <div>
      <label class="ds-form-label">Type full name to confirm</label>
      <input
        class="ds-input"
        bind:value={deleteTableForm.typedName}
        placeholder={`${deleteTableForm.database}.${deleteTableForm.name}`}
      />
    </div>

    <div class="flex items-center justify-end gap-2 pt-1">
      <button
        type="button"
        class="ds-btn-outline"
        onclick={() => (deleteTableSheetOpen = false)}>Cancel</button
      >
      <button
        type="submit"
        class="inline-flex items-center justify-center gap-1.5 rounded px-3 py-1.5 text-[13px] font-medium text-white bg-red-600 border border-red-500 transition-colors hover:bg-red-700 disabled:opacity-60 disabled:cursor-not-allowed"
        disabled={deleteTableSubmitting ||
          deleteTableForm.typedName.trim() !==
            `${deleteTableForm.database}.${deleteTableForm.name}`}
      >
        <Trash2 size={14} />
        {deleteTableSubmitting ? "Deleting..." : "Delete Table"}
      </button>
    </div>
  </form>
</Sheet>
