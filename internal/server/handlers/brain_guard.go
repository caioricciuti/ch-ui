package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// errGuardrailEval is returned when the policies could not be evaluated.
// Brain fails closed here, like the SQL editor.
var errGuardrailEval = errors.New("could not evaluate governance guardrails; the query was not run")

// guardrailBlockedError carries the policy that blocked a Brain query.
type guardrailBlockedError struct {
	Block *governance.GuardrailBlock
}

func (e *guardrailBlockedError) Error() string {
	if e.Block == nil {
		return "query blocked by governance policy"
	}
	return fmt.Sprintf("query blocked by governance policy %q (severity %s): %s", e.Block.PolicyName, e.Block.Severity, e.Block.Detail)
}

// guardrailService returns the configured service, or one built on the
// handler's database, so Brain enforces the same policies as the editor
// even when the server did not wire one in.
func (h *BrainHandler) guardrailService() *governance.GuardrailService {
	h.guardOnce.Do(func() {
		if h.Guardrails != nil {
			h.guard = h.Guardrails
			return
		}
		if h.DB != nil {
			h.guard = governance.NewGuardrailService(governance.NewStore(h.DB), h.DB)
		}
	})
	return h.guard
}

// guardrailCheck returns the governance check for the session's queries, or
// nil when governance does not apply (no Pro license), matching the
// editor's guardrailsEnabled.
func (h *BrainHandler) guardrailCheck(session *middleware.SessionInfo, endpoint string) func(sql string) error {
	if session == nil || (h.Config != nil && !h.Config.IsPro()) {
		return nil
	}
	svc := h.guardrailService()
	if svc == nil {
		return nil
	}
	connID, chUser := session.ConnectionID, session.ClickhouseUser
	return func(sql string) error {
		decision, err := svc.EvaluateQuery(connID, chUser, sql, endpoint)
		if err != nil {
			slog.Error("Brain guardrail evaluation failed", "connection", connID, "endpoint", endpoint, "error", err)
			return errGuardrailEval
		}
		if decision.Allowed {
			return nil
		}
		return &guardrailBlockedError{Block: decision.Block}
	}
}

// queryRecorder writes Brain's user-facing queries to the person's query
// history, tagged with the "brain" source.
func (h *BrainHandler) queryRecorder(session *middleware.SessionInfo) func(sql, status, errMsg string, elapsed time.Duration, rows int64) {
	if session == nil || h.DB == nil {
		return nil
	}
	connID, chUser, actor := session.ConnectionID, session.ClickhouseUser, middleware.Actor(session)
	return func(sql, status, errMsg string, elapsed time.Duration, rows int64) {
		if len(errMsg) > maxHistoryErrorLen {
			errMsg = truncateUTF8(errMsg, maxHistoryErrorLen) + " [truncated]"
		}
		if err := h.DB.CreateQueryHistoryEntry(database.CreateQueryHistoryParams{
			ConnectionID: connID,
			User:         chUser,
			Actor:        actor,
			QueryText:    sql,
			Status:       status,
			ErrorMessage: errMsg,
			ElapsedMS:    elapsed.Milliseconds(),
			RowsReturned: rows,
			Source:       "brain",
		}); err != nil {
			slog.Warn("Failed to record Brain query history", "error", err)
		}
	}
}

// writeBrainPolicyBlocked answers like the editor when a policy blocks a
// query, so the UI shows the same policy_blocked error.
func writeBrainPolicyBlocked(w http.ResponseWriter, block *governance.GuardrailBlock) {
	if block == nil {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"success": false,
			"error":   "Query blocked by governance policy",
			"code":    "policy_blocked",
		})
		return
	}
	writeJSON(w, http.StatusForbidden, map[string]interface{}{
		"success":          false,
		"error":            block.Detail,
		"code":             "policy_blocked",
		"policy_id":        block.PolicyID,
		"policy_name":      block.PolicyName,
		"severity":         block.Severity,
		"enforcement_mode": block.EnforcementMode,
		"violation_id":     block.ViolationID,
	})
}
