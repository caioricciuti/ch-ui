// Package lineage builds the data lineage graph of a ClickHouse server from
// its system tables: which tables feed which materialized views, views and
// dictionaries, and where those write.
//
// It reads only metadata the caller already fetched (system.tables, and when
// readable system.dictionaries and system.view_refreshes). It never reads
// system.query_log and never runs in the background.
//
// Every edge points the way data flows, from source to consumer:
//
//	source table  -triggers-> MV            dependencies_* of the source (system)
//	MV            -writes->   TO target     TO clause of the definition (parsed)
//	read table    -reads->    view or MV    FROM, JOIN, subqueries, dictGet,
//	                                        table functions, DEPENDS ON (parsed)
//	Buffer        -routes->   target table  flushes go to the target (system)
//	Distributed   -routes->   local table   inserts go to the local table (system)
//	member table  -routes->   Merge         Merge only reads its members (system)
//	source        -sources->  dictionary    SOURCE(...) clause (parsed), or
//	                                        system.dictionaries.source (system)
//
// Confidence "system" means ClickHouse states the edge in a metadata column
// (dependencies_*, engine_full, system.dictionaries); "parsed" means it was
// read from a view, MV or dictionary definition by this package's tokenizer.
// When both describe the same pair, the system edge wins: an MV's source
// table is listed as triggers, not reads.
//
// An MV without TO stores rows in an inner table (.inner_id.<uuid> or
// .inner.<name>). That table is folded into the MV node, which carries its
// rows and bytes, and is never a node of its own.
//
// Objects that a definition names but no visible row matches are "missing";
// MVs listed in a visible table's dependencies that the user cannot see are
// "hidden". Tables with an engine that reads outside ClickHouse (Kafka, S3,
// MySQL, ...) are "external" nodes with a descriptor such as
// kafka://broker/topic. Table functions and dictionary sources outside
// ClickHouse become nodes whose id starts with "external:" and that have no
// database. Descriptors never include credentials.
//
// The parser is a small tokenizer, not regular expressions: strings,
// comments and quoted identifiers cannot fake a FROM. It tolerates SQL it
// does not understand by producing fewer references, never an error.
package lineage
