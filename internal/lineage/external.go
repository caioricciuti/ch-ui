package lineage

import "strings"

// External descriptors name where data comes from outside ClickHouse, like
// kafka://broker/topic or an object store URL. They never carry credentials:
// only host, path and object arguments are used, and user info and query
// strings are stripped from URLs (a presigned URL's signature is in its query).

// externalEngine describes a table whose engine reads from outside
// ClickHouse, or returns "" for an engine that stores data in ClickHouse.
func externalEngine(engine string, spec engineSpec) string {
	e := strings.ToLower(engine)
	set := func(k string) string { return spec.settings[k] }
	arg := func(i int) string { return spec.arg(i, "") }
	switch e {
	case "kafka":
		broker, topic := set("kafka_broker_list"), set("kafka_topic_list")
		if broker == "" {
			broker, topic = arg(0), arg(1)
		}
		return joinDescriptor("kafka", broker, topic)
	case "rabbitmq":
		host := set("rabbitmq_host_port")
		if host == "" {
			host = set("rabbitmq_address")
		}
		return joinDescriptor("rabbitmq", host, set("rabbitmq_exchange_name"))
	case "nats":
		return joinDescriptor("nats", strings.TrimPrefix(set("nats_url"), "nats://"), set("nats_subjects"))
	case "file":
		// File(Format) keeps its data in the table directory; File(Format, path) does not.
		if len(spec.args) < 2 {
			return ""
		}
		return "file://" + sanitizeLocation(arg(1))
	}
	if !externalEngines[e] && !strings.HasPrefix(e, "iceberg") && !strings.HasPrefix(e, "deltalake") && !strings.HasPrefix(e, "hudi") {
		return ""
	}
	if d := externalFunction(e, spec.args); d != "" {
		return d
	}
	return e
}

var externalEngines = map[string]bool{
	"s3": true, "s3queue": true, "azurequeue": true, "url": true, "mysql": true, "postgresql": true,
	"mongodb": true, "redis": true, "hdfs": true, "azureblobstorage": true, "jdbc": true, "odbc": true,
	"sqlite": true, "hive": true, "externaldistributed": true, "materializedpostgresql": true,
	"filelog": true, "executable": true, "executablepool": true, "arrowflight": true, "ytsaurus": true,
	"gcs": true, "cosn": true, "oss": true,
}

// externalFunction describes a table function (or the matching engine) that
// reads from outside ClickHouse, or returns "" when fn is not one.
func externalFunction(fn string, args []argValue) string {
	at := func(i int) argValue {
		if i < len(args) {
			return args[i]
		}
		return argValue{}
	}
	// A bare identifier first argument is a named collection.
	if first := at(0); first.text != "" && !first.literal {
		if isExternalFunction(fn) {
			return fn + ":" + first.text
		}
		return ""
	}
	base := strings.TrimSuffix(fn, "cluster")
	switch {
	case base == "s3" || base == "gcs" || base == "cosn" || base == "oss" || base == "url" || base == "hdfs" ||
		base == "azureblobstorage" || base == "s3queue" || base == "azurequeue" || base == "arrowflight" ||
		strings.HasPrefix(base, "iceberg") || strings.HasPrefix(base, "deltalake") || strings.HasPrefix(base, "hudi"):
		if base == "azureblobstorage" && at(1).text != "" {
			// azureBlobStorage(connection, container, blobpath, ...)
			return joinDescriptor("azure", sanitizeLocation(at(0).text), at(1).text, at(2).text)
		}
		if loc := sanitizeLocation(at(0).text); loc != "" {
			return loc
		}
		return base
	case base == "file" || base == "filelog":
		if at(0).text == "" {
			return "file"
		}
		return "file://" + at(0).text
	case base == "mysql" || base == "postgresql" || base == "mongodb" || base == "materializedpostgresql" ||
		base == "externaldistributed" || base == "hive":
		host := sanitizeLocation(at(0).text)
		if base == "externaldistributed" {
			// ExternalDistributed('engine', 'host:port', 'db', 'table', ...)
			return joinDescriptor(strings.ToLower(at(0).text), sanitizeLocation(at(1).text), qualified(at(2).text, at(3).text))
		}
		return joinDescriptor(base, host, qualified(at(1).text, at(2).text))
	case base == "jdbc" || base == "odbc":
		return joinDescriptor(base, sanitizeLocation(at(0).text), qualified(at(1).text, at(2).text))
	case base == "sqlite":
		return joinDescriptor("sqlite", at(0).text, at(1).text)
	case base == "redis":
		return joinDescriptor("redis", sanitizeLocation(at(0).text))
	case base == "executable" || base == "executablepool":
		script := at(0).text
		if i := strings.IndexByte(script, ' '); i >= 0 {
			script = script[:i]
		}
		return joinDescriptor("executable", script)
	case base == "ytsaurus":
		return joinDescriptor("ytsaurus", sanitizeLocation(at(0).text), at(1).text)
	}
	return ""
}

func isExternalFunction(fn string) bool {
	return externalFunction(fn, []argValue{{text: "x", literal: true}}) != ""
}

// joinDescriptor builds scheme://part/part from the non-empty parts. A part
// that already has a scheme keeps it.
func joinDescriptor(scheme string, parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		p = strings.Trim(p, "/")
		if p == "" || p == "." {
			continue
		}
		if b.Len() == 0 {
			if !strings.Contains(p, "://") {
				b.WriteString(scheme + "://")
			}
		} else {
			b.WriteByte('/')
		}
		b.WriteString(p)
	}
	if b.Len() == 0 {
		return scheme
	}
	return b.String()
}

// sanitizeLocation drops user info, query string and fragment from a URL or
// host so credentials and signatures never leave the server.
func sanitizeLocation(s string) string {
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	rest, scheme := s, ""
	if i := strings.Index(s, "://"); i >= 0 {
		scheme, rest = s[:i+3], s[i+3:]
	}
	authority := rest
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		authority = rest[:i]
	}
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		rest = rest[at+1:]
	}
	return scheme + rest
}
