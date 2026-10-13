package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// exportFormat describes one output format offered by POST /export.
type exportFormat struct {
	ext  string
	mime string
}

// exportFormats are the ClickHouse output formats a full export may use.
// XLSX is not here: ClickHouse 25.8 has no native XLSX output format.
var exportFormats = map[string]exportFormat{
	"CSVWithNames": {ext: "csv", mime: "text/csv; charset=utf-8"},
	"TSVWithNames": {ext: "tsv", mime: "text/tab-separated-values; charset=utf-8"},
	"JSONEachRow":  {ext: "jsonl", mime: "application/x-ndjson"},
	"Parquet":      {ext: "parquet", mime: "application/vnd.apache.parquet"},
}

type exportQueryRequest struct {
	SQL    string            `json:"sql"`
	Format string            `json:"format"`
	Params map[string]string `json:"params"`
}

// exportFormatClauseRe matches a trailing FORMAT clause (optionally followed
// by SETTINGS), which would fight the format the export asks for.
var exportFormatClauseRe = regexp.MustCompile(`(?is)\bFORMAT\s+\w+(\s+SETTINGS\s+[^()]*)?$`)

// exportFilename is a fixed, safe download name: nothing from the request
// reaches the header.
func exportFilename(ext string, now time.Time) string {
	return "ch-ui-export-" + now.UTC().Format("20060102-150405") + "." + ext
}

// exportSettings are the ClickHouse settings every export runs with: read
// only, and capped so one export cannot pull an unbounded result.
func exportSettings(maxBytes, maxRows int64, params map[string]string) map[string]string {
	settings := map[string]string{
		"readonly":             "1",
		"max_result_rows":      strconv.FormatInt(maxRows, 10),
		"max_result_bytes":     strconv.FormatInt(maxBytes, 10),
		"result_overflow_mode": "throw",
	}
	for k, v := range buildParamSettings(params) {
		settings[k] = v
	}
	return settings
}

// exportErrorMessage explains a limit error in the server's own terms.
func exportErrorMessage(err error, maxBytes, maxRows int64) string {
	msg := err.Error()
	if strings.Contains(msg, "TOO_MANY_ROWS_OR_BYTES") || strings.Contains(msg, "Limit for result exceeded") {
		return fmt.Sprintf("The result is larger than the export limit (%s rows, %s). Narrow the query or ask an admin to raise export_max_rows / export_max_bytes. %s",
			strconv.FormatInt(maxRows, 10), formatBytesIEC(maxBytes), msg)
	}
	return msg
}

var clickhouseExceptionRe = regexp.MustCompile(`Code: \d+\. DB::Exception`)

// exportErrorStatus is 422 when ClickHouse answered and refused the query
// (syntax, access, limits) and 502 when it could not be reached at all.
func exportErrorStatus(err error) int {
	if clickhouseExceptionRe.MatchString(err.Error()) {
		return http.StatusUnprocessableEntity
	}
	return http.StatusBadGateway
}

func formatBytesIEC(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ExportQuery handles POST /export: it re-runs one statement on ClickHouse
// with FORMAT <format> and streams the raw bytes back as a download. It goes
// through the same guardrails, credentials, bind parameters, history and audit
// as Run, always with readonly=1 and the configured size limits.
//
// Errors before the first byte are JSON with a 4xx/5xx status. An error after
// the download started aborts the connection, so the browser sees a failed
// download instead of a file with an error message at its end.
func (h *QueryHandler) ExportQuery(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var req exportQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	query := strings.TrimRight(strings.TrimSpace(req.SQL), "; \n\t")
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query is required")
		return
	}
	format, ok := exportFormats[req.Format]
	if !ok {
		writeError(w, http.StatusBadRequest, "Unsupported export format; use CSVWithNames, TSVWithNames, JSONEachRow or Parquet")
		return
	}
	if exportFormatClauseRe.MatchString(query) {
		writeError(w, http.StatusBadRequest, "Remove the FORMAT clause from the statement; the export sets the format")
		return
	}
	if !h.enforceGuardrailsForQuery(w, r, query, r.URL.Path) {
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	maxBytes, maxRows := h.Config.ExportLimits()
	settings := exportSettings(maxBytes, maxRows, req.Params)
	start := time.Now()
	actor := middleware.Actor(session)

	history := func(status, errMsg string) {
		h.recordQueryHistory(session.ConnectionID, session.ClickhouseUser, actor, query, status, errMsg, time.Since(start).Milliseconds(), 0)
	}
	audit := func(outcome string, bytes int64) {
		preview := query
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		details := fmt.Sprintf("%s, %s, %d bytes: %s", req.Format, outcome, bytes, preview)
		ip := getClientIP(r)
		h.background(func() {
			h.DB.CreateAuditLog(database.AuditLogParams{
				Action:         "query.export",
				Username:       strPtr(actor),
				ClickhouseUser: strPtr(session.ClickhouseUser),
				ConnectionID:   strPtr(session.ConnectionID),
				Details:        strPtr(details),
				IPAddress:      strPtr(ip),
			})
		})
	}

	ctx := r.Context()

	// A user whose ClickHouse profile is readonly=1 may not change any other
	// setting. As for Run, a refused setting of ours is dropped and the export
	// retried; readonly and bind parameters are always accepted, and the byte
	// cap below still holds.
	var (
		requestID string
		stream    *tunnel.PendingStreamRequest
		first     []byte
		gotFirst  bool
		finished  bool
	)
	for attempt := 0; ; attempt++ {
		requestID, stream, err = h.Gateway.ExecuteExportQuery(session.ConnectionID, query, session.ClickhouseUser, password, req.Format, settings)
		if err != nil {
			status := exportErrorStatus(err)
			if errors.Is(err, tunnel.ErrExportUnsupported) {
				status = http.StatusConflict
			}
			history("error", err.Error())
			audit("failed", 0)
			writeError(w, status, err.Error())
			return
		}

		// Wait for the first bytes, the end of an empty export, or an error.
		var firstErr error
		select {
		case chunk := <-stream.ChunkCh:
			first, firstErr = tunnel.DecodeExportChunk(chunk)
			gotFirst = firstErr == nil
		case <-stream.Done():
			select {
			case chunk := <-stream.ChunkCh:
				first, firstErr = tunnel.DecodeExportChunk(chunk)
				gotFirst = firstErr == nil
			default:
				select {
				case <-stream.DoneCh:
					finished = true
				case firstErr = <-stream.ErrorCh:
				default:
					firstErr = errors.New("export ended without a result")
				}
			}
		case <-ctx.Done():
			h.Gateway.CancelStreamQuery(session.ConnectionID, requestID)
			h.Gateway.CleanupStream(session.ConnectionID, requestID)
			history("cancelled", "")
			audit("cancelled", 0)
			return
		}

		if firstErr == nil {
			break
		}
		h.Gateway.CleanupStream(session.ConnectionID, requestID)
		name := tunnel.RefusedSetting(firstErr)
		if _, ours := settings[name]; attempt < 8 && ours && name != "readonly" && !strings.HasPrefix(name, "param_") && !sqlMentionsSettingName(query, name) {
			settings = withoutSettingName(settings, name)
			continue
		}
		msg := exportErrorMessage(firstErr, maxBytes, maxRows)
		history("error", msg)
		audit("failed", 0)
		writeError(w, exportErrorStatus(firstErr), msg)
		return
	}
	defer h.Gateway.CleanupStream(session.ConnectionID, requestID)

	// An export can outlast the server's write timeout; the byte cap and the
	// client's own cancel bound it instead.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", format.mime)
	w.Header().Set("Content-Disposition", `attachment; filename="`+exportFilename(format.ext, time.Now())+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	var written int64
	// fail ends a download that already started: the client must not keep a
	// truncated file as if it were complete, so the connection is aborted.
	fail := func(status, msg string) {
		h.Gateway.CancelStreamQuery(session.ConnectionID, requestID)
		history(status, msg)
		audit(status, written)
		if msg != "" {
			slog.Warn("Export aborted", "connection", session.ConnectionID, "reason", msg)
		}
		panic(http.ErrAbortHandler)
	}
	write := func(b []byte) {
		if written+int64(len(b)) > maxBytes {
			fail("error", fmt.Sprintf("Export stopped at the %s export limit", formatBytesIEC(maxBytes)))
		}
		if _, err := w.Write(b); err != nil {
			// The client went away mid-download.
			fail("cancelled", "")
		}
		written += int64(len(b))
		if flusher != nil {
			flusher.Flush()
		}
	}

	if finished {
		history("success", "")
		audit("completed", 0)
		return
	}
	if gotFirst {
		write(first)
	}

	writeChunk := func(chunk json.RawMessage) {
		b, err := tunnel.DecodeExportChunk(chunk)
		if err != nil {
			fail("error", err.Error())
		}
		write(b)
	}
readChunks:
	for {
		select {
		case chunk := <-stream.ChunkCh:
			writeChunk(chunk)
		case <-stream.Done():
			break readChunks
		case <-ctx.Done():
			h.Gateway.CancelStreamQuery(session.ConnectionID, requestID)
			history("cancelled", "")
			audit("cancelled", written)
			return
		}
	}
	for drained := false; !drained; {
		select {
		case chunk := <-stream.ChunkCh:
			writeChunk(chunk)
		default:
			drained = true
		}
	}

	select {
	case <-stream.DoneCh:
		history("success", "")
		audit("completed", written)
	case err := <-stream.ErrorCh:
		fail("error", exportErrorMessage(err, maxBytes, maxRows))
	default:
		fail("error", "export ended without a result")
	}
}

// sqlMentionsSettingName reports whether the statement names setting itself
// (for example in a SETTINGS clause); then the refusal is about the user's own
// change, and dropping ours would let the statement lift the cap.
func sqlMentionsSettingName(sql, setting string) bool {
	re := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(setting) + `($|[^A-Za-z0-9_])`)
	return re.MatchString(sql)
}

func withoutSettingName(settings map[string]string, name string) map[string]string {
	out := make(map[string]string, len(settings))
	for k, v := range settings {
		if k != name {
			out[k] = v
		}
	}
	return out
}
