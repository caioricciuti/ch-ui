package governance

// ── Sensitivity tag constants ────────────────────────────────────────────────

type SensitivityTag string

const (
	TagPII       SensitivityTag = "PII"
	TagFinancial SensitivityTag = "FINANCIAL"
	TagInternal  SensitivityTag = "INTERNAL"
	TagPublic    SensitivityTag = "PUBLIC"
	TagCritical  SensitivityTag = "CRITICAL"
)

var ValidTags = map[SensitivityTag]bool{
	TagPII: true, TagFinancial: true, TagInternal: true,
	TagPublic: true, TagCritical: true,
}

// ── Sync types ───────────────────────────────────────────────────────────────

type SyncType string

const (
	SyncMetadata SyncType = "metadata"
	SyncQueryLog SyncType = "query_log"
	SyncAccess   SyncType = "access"
)

// ── Schema change types ──────────────────────────────────────────────────────

type SchemaChangeType string

const (
	ChangeDatabaseAdded     SchemaChangeType = "database_added"
	ChangeDatabaseRemoved   SchemaChangeType = "database_removed"
	ChangeTableAdded        SchemaChangeType = "table_added"
	ChangeTableRemoved      SchemaChangeType = "table_removed"
	ChangeColumnAdded       SchemaChangeType = "column_added"
	ChangeColumnRemoved     SchemaChangeType = "column_removed"
	ChangeColumnTypeChanged SchemaChangeType = "column_type_changed"
)

// ── Edge types ───────────────────────────────────────────────────────────────

type EdgeType string

const (
	EdgeSelectFrom     EdgeType = "select_from"
	EdgeInsertSelect   EdgeType = "insert_select"
	EdgeCreateAsSelect EdgeType = "create_as_select"
)

// ── Model structs ────────────────────────────────────────────────────────────

type SyncState struct {
	ID           string  `json:"id"`
	ConnectionID string  `json:"connection_id"`
	SyncType     string  `json:"sync_type"`
	LastSyncedAt *string `json:"last_synced_at"`
	Watermark    *string `json:"watermark"`
	Status       string  `json:"status"`
	LastError    *string `json:"last_error"`
	RowCount     int     `json:"row_count"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type GovDatabase struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"`
	Name         string `json:"name"`
	Engine       string `json:"engine"`
	FirstSeen    string `json:"first_seen"`
	LastUpdated  string `json:"last_updated"`
	IsDeleted    bool   `json:"is_deleted"`
}

type GovTable struct {
	ID             string   `json:"id"`
	ConnectionID   string   `json:"connection_id"`
	DatabaseName   string   `json:"database_name"`
	TableName      string   `json:"table_name"`
	Engine         string   `json:"engine"`
	TableUUID      string   `json:"table_uuid"`
	TotalRows      int64    `json:"total_rows"`
	TotalBytes     int64    `json:"total_bytes"`
	PartitionCount int      `json:"partition_count"`
	FirstSeen      string   `json:"first_seen"`
	LastUpdated    string   `json:"last_updated"`
	IsDeleted      bool     `json:"is_deleted"`
	Tags           []string `json:"tags,omitempty"`
}

type GovColumn struct {
	ID                string   `json:"id"`
	ConnectionID      string   `json:"connection_id"`
	DatabaseName      string   `json:"database_name"`
	TableName         string   `json:"table_name"`
	ColumnName        string   `json:"column_name"`
	ColumnType        string   `json:"column_type"`
	ColumnPosition    int      `json:"column_position"`
	DefaultKind       *string  `json:"default_kind"`
	DefaultExpression *string  `json:"default_expression"`
	Comment           *string  `json:"comment"`
	FirstSeen         string   `json:"first_seen"`
	LastUpdated       string   `json:"last_updated"`
	IsDeleted         bool     `json:"is_deleted"`
	Tags              []string `json:"tags,omitempty"`
}

type SchemaChange struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"`
	ChangeType   string `json:"change_type"`
	DatabaseName string `json:"database_name"`
	TableName    string `json:"table_name"`
	ColumnName   string `json:"column_name"`
	OldValue     string `json:"old_value"`
	NewValue     string `json:"new_value"`
	DetectedAt   string `json:"detected_at"`
	CreatedAt    string `json:"created_at"`
}

type QueryLogEntry struct {
	ID             string  `json:"id"`
	ConnectionID   string  `json:"connection_id"`
	QueryID        string  `json:"query_id"`
	User           string  `json:"ch_user"`
	QueryText      string  `json:"query_text"`
	NormalizedHash string  `json:"normalized_hash"`
	QueryKind      string  `json:"query_kind"`
	EventTime      string  `json:"event_time"`
	DurationMs     int64   `json:"duration_ms"`
	ReadRows       int64   `json:"read_rows"`
	ReadBytes      int64   `json:"read_bytes"`
	ResultRows     int64   `json:"result_rows"`
	WrittenRows    int64   `json:"written_rows"`
	WrittenBytes   int64   `json:"written_bytes"`
	MemoryUsage    int64   `json:"memory_usage"`
	TablesUsed     string  `json:"tables_used"`
	IsError        bool    `json:"is_error"`
	ErrorMessage   *string `json:"error_message"`
	CreatedAt      string  `json:"created_at"`
}

type LineageEdge struct {
	ID             string `json:"id"`
	ConnectionID   string `json:"connection_id"`
	SourceDatabase string `json:"source_database"`
	SourceTable    string `json:"source_table"`
	TargetDatabase string `json:"target_database"`
	TargetTable    string `json:"target_table"`
	QueryID        string `json:"query_id"`
	User           string `json:"ch_user"`
	EdgeType       string `json:"edge_type"`
	DetectedAt     string `json:"detected_at"`
}

type TagEntry struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"`
	ObjectType   string `json:"object_type"`
	DatabaseName string `json:"database_name"`
	TableName    string `json:"table_name"`
	ColumnName   string `json:"column_name"`
	Tag          string `json:"tag"`
	TaggedBy     string `json:"tagged_by"`
	CreatedAt    string `json:"created_at"`
}

type ChUser struct {
	ID           string  `json:"id"`
	ConnectionID string  `json:"connection_id"`
	Name         string  `json:"name"`
	AuthType     *string `json:"auth_type"`
	HostIP       *string `json:"host_ip"`
	DefaultRoles *string `json:"default_roles"`
	FirstSeen    string  `json:"first_seen"`
	LastUpdated  string  `json:"last_updated"`
}

type ChRole struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"`
	Name         string `json:"name"`
	FirstSeen    string `json:"first_seen"`
	LastUpdated  string `json:"last_updated"`
}

type RoleGrant struct {
	ID              string `json:"id"`
	ConnectionID    string `json:"connection_id"`
	UserName        string `json:"user_name"`
	GrantedRoleName string `json:"granted_role_name"`
	IsDefault       bool   `json:"is_default"`
	WithAdminOption bool   `json:"with_admin_option"`
	FirstSeen       string `json:"first_seen"`
	LastUpdated     string `json:"last_updated"`
}

type Grant struct {
	ID              string  `json:"id"`
	ConnectionID    string  `json:"connection_id"`
	UserName        *string `json:"user_name"`
	RoleName        *string `json:"role_name"`
	AccessType      string  `json:"access_type"`
	GrantDatabase   *string `json:"grant_database"`
	GrantTable      *string `json:"grant_table"`
	GrantColumn     *string `json:"grant_column"`
	IsPartialRevoke bool    `json:"is_partial_revoke"`
	GrantOption     bool    `json:"grant_option"`
	FirstSeen       string  `json:"first_seen"`
	LastUpdated     string  `json:"last_updated"`
}

type AccessMatrixEntry struct {
	ID            string  `json:"id"`
	ConnectionID  string  `json:"connection_id"`
	UserName      string  `json:"user_name"`
	RoleName      *string `json:"role_name"`
	DatabaseName  *string `json:"database_name"`
	TableName     *string `json:"table_name"`
	Privilege     string  `json:"privilege"`
	IsDirectGrant bool    `json:"is_direct_grant"`
	LastQueryTime *string `json:"last_query_time"`
}

type Policy struct {
	ID             string  `json:"id"`
	ConnectionID   string  `json:"connection_id"`
	Name           string  `json:"name"`
	Description    *string `json:"description"`
	ObjectType     string  `json:"object_type"`
	ObjectDatabase *string `json:"object_database"`
	ObjectTable    *string `json:"object_table"`
	ObjectColumn   *string `json:"object_column"`
	RequiredRole   string  `json:"required_role"`
	Severity       string  `json:"severity"`
	Enabled        bool    `json:"enabled"`
	CreatedBy      *string `json:"created_by"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type PolicyViolation struct {
	ID              string `json:"id"`
	ConnectionID    string `json:"connection_id"`
	PolicyID        string `json:"policy_id"`
	QueryLogID      string `json:"query_log_id"`
	User            string `json:"ch_user"`
	ViolationDetail string `json:"violation_detail"`
	Severity        string `json:"severity"`
	DetectedAt      string `json:"detected_at"`
	CreatedAt       string `json:"created_at"`
	// Joined fields
	PolicyName string `json:"policy_name,omitempty"`
}

type ObjectComment struct {
	ID           string  `json:"id"`
	ConnectionID string  `json:"connection_id"`
	ObjectType   string  `json:"object_type"`
	DatabaseName string  `json:"database_name"`
	TableName    string  `json:"table_name"`
	ColumnName   string  `json:"column_name"`
	CommentText  string  `json:"comment_text"`
	CreatedBy    *string `json:"created_by"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type Incident struct {
	ID              string  `json:"id"`
	ConnectionID    string  `json:"connection_id"`
	SourceType      string  `json:"source_type"`
	SourceRef       *string `json:"source_ref"`
	DedupeKey       *string `json:"dedupe_key"`
	Title           string  `json:"title"`
	Severity        string  `json:"severity"`
	Status          string  `json:"status"`
	Assignee        *string `json:"assignee"`
	Details         *string `json:"details"`
	ResolutionNote  *string `json:"resolution_note"`
	OccurrenceCount int     `json:"occurrence_count"`
	FirstSeenAt     string  `json:"first_seen_at"`
	LastSeenAt      string  `json:"last_seen_at"`
	ResolvedAt      *string `json:"resolved_at"`
	CreatedBy       *string `json:"created_by"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type IncidentComment struct {
	ID          string  `json:"id"`
	IncidentID  string  `json:"incident_id"`
	CommentText string  `json:"comment_text"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
}

// ── Summary types ────────────────────────────────────────────────────────────

type GovernanceOverview struct {
	DatabaseCount     int               `json:"database_count"`
	TableCount        int               `json:"table_count"`
	ColumnCount       int               `json:"column_count"`
	TaggedTableCount  int               `json:"tagged_table_count"`
	UserCount         int               `json:"user_count"`
	RoleCount         int               `json:"role_count"`
	QueryCount24h     int               `json:"query_count_24h"`
	LineageEdgeCount  int               `json:"lineage_edge_count"`
	PolicyCount       int               `json:"policy_count"`
	ViolationCount    int               `json:"violation_count"`
	IncidentCount     int               `json:"incident_count"`
	SchemaChangeCount int               `json:"schema_change_count"`
	SyncStates        []SyncState       `json:"sync_states"`
	RecentChanges     []SchemaChange    `json:"recent_changes"`
	RecentViolations  []PolicyViolation `json:"recent_violations"`
}

type OverPermission struct {
	UserName       string  `json:"user_name"`
	RoleName       *string `json:"role_name"`
	DatabaseName   *string `json:"database_name"`
	TableName      *string `json:"table_name"`
	Privilege      string  `json:"privilege"`
	LastQueryTime  *string `json:"last_query_time"`
	DaysSinceQuery *int    `json:"days_since_query"`
	Reason         string  `json:"reason"`
}

// ── Sync result types ────────────────────────────────────────────────────────

type SyncResult struct {
	MetadataResult *MetadataSyncResult `json:"metadata,omitempty"`
	MetadataError  string              `json:"metadata_error,omitempty"`
	QueryLogResult *QueryLogSyncResult `json:"query_log,omitempty"`
	QueryLogError  string              `json:"query_log_error,omitempty"`
	AccessResult   *AccessSyncResult   `json:"access,omitempty"`
	AccessError    string              `json:"access_error,omitempty"`
}

type MetadataSyncResult struct {
	DatabasesSynced int `json:"databases_synced"`
	TablesSynced    int `json:"tables_synced"`
	ColumnsSynced   int `json:"columns_synced"`
	SchemaChanges   int `json:"schema_changes"`
}

type QueryLogSyncResult struct {
	QueriesIngested   int    `json:"queries_ingested"`
	LineageEdgesFound int    `json:"lineage_edges_found"`
	ViolationsFound   int    `json:"violations_found"`
	NewWatermark      string `json:"new_watermark"`
}

type AccessSyncResult struct {
	UsersSynced     int `json:"users_synced"`
	RolesSynced     int `json:"roles_synced"`
	GrantsSynced    int `json:"grants_synced"`
	MatrixEntries   int `json:"matrix_entries"`
	OverPermissions int `json:"over_permissions"`
}

// ── Credentials holder ───────────────────────────────────────────────────────

type CHCredentials struct {
	ConnectionID string
	User         string
	Password     string
}

// ── Lineage graph (for API response) ─────────────────────────────────────────

type LineageNode struct {
	ID       string `json:"id"`
	Database string `json:"database"`
	Table    string `json:"table"`
	Type     string `json:"type"` // "source", "target", "current"
}

type LineageGraph struct {
	Nodes []LineageNode `json:"nodes"`
	Edges []LineageEdge `json:"edges"`
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func StrPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
