package handlers

import (
	"net/http"
	"regexp"
	"strconv"
)

// chExceptionCode finds the exception code in a ClickHouse answer, which the
// connector reports as "ClickHouse error: Code: 497. DB::Exception: ...".
var chExceptionCode = regexp.MustCompile(`Code: (\d+)\.`)

// clickhouseStatus maps a failed statement to the HTTP status the caller
// should see. A missing grant or a bad statement is the caller's problem and
// must not come back as 502, which the UI reads as ClickHouse being
// unreachable. Anything without a recognised code keeps the gateway status.
func clickhouseStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	switch clickhouseCode(err) {
	case 0:
		return http.StatusBadGateway
	case 497, 164: // ACCESS_DENIED, READONLY
		return http.StatusForbidden
	case 159: // TIMEOUT_EXCEEDED
		return http.StatusGatewayTimeout
	case 202: // TOO_MANY_SIMULTANEOUS_QUERIES
		return http.StatusServiceUnavailable
	case 15, // DUPLICATE_COLUMN
		16, // NO_SUCH_COLUMN_IN_TABLE
		36, // BAD_ARGUMENTS
		43, // ILLEGAL_TYPE_OF_ARGUMENT
		44, // ILLEGAL_COLUMN
		47, // UNKNOWN_IDENTIFIER
		48, // NOT_IMPLEMENTED
		53, // TYPE_MISMATCH
		57, // TABLE_ALREADY_EXISTS
		60, // UNKNOWN_TABLE
		62, // SYNTAX_ERROR
		81, // UNKNOWN_DATABASE
		82: // DATABASE_ALREADY_EXISTS
		return http.StatusBadRequest
	}
	return http.StatusBadGateway
}

// clickhouseCode is the exception code of a failed statement, or 0 when the
// error carries none (the connection failed, say).
func clickhouseCode(err error) int {
	if err == nil {
		return 0
	}
	m := chExceptionCode.FindStringSubmatch(err.Error())
	if m == nil {
		return 0
	}
	code, _ := strconv.Atoi(m[1])
	return code
}
