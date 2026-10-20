---
title: Ship logs
description: Send OpenTelemetry logs straight to CH-UI in one step, then search them in Telemetry
---

CH-UI can receive logs itself and write them to ClickHouse, so you do not
need a collector that talks to ClickHouse. One setup creates the table, an
ingest pipeline, a key and the Telemetry source. You need the analyst or
admin role; viewers see who to ask.

## Set it up

1. Click **Visualize** in the rail, then **Telemetry**, then **Logs**.
2. Click **Send data here**. It is in the page header, and in the middle of
   the page while you have no logs source yet.
3. Check the three fields:
   - **Database**: where the table goes. `default` is preselected.
   - **Table**: `otel_logs` by default. CH-UI creates it in the
     OpenTelemetry layout, or reuses it if it already has that shape.
   - **Retention in days**: rows older than this are dropped. 30 by default.
4. Click **Set up**.

## Copy the key

The sheet now shows the new ingest key, with a warning above it. Copy it with
the button next to it and store it in your secret manager. CH-UI keeps only
its prefix: you will not see the key again. Lost it? Create another one later
from the same sheet with **Create key**.

## Point your shipper at CH-UI

Below the key, pick your shipper. The snippet already holds your CH-UI URL
and the key.

- **OpenTelemetry Collector**: add the `otlphttp` exporter from the snippet
  to your collector config and to its `logs` pipeline, then restart it.
- **OTel SDK env vars**: export the variables in the environment of the
  service you want logs from, then restart it.
- **curl**: run the command to send one test record, `hello from curl`.
- **Fluent Bit**: add the output block to `fluent-bit.conf`.

Click **Copy snippet**, paste it where it belongs, and close the sheet.

## See logs arrive

1. Back on **Telemetry**, **Logs**, pick the new source in the source list
   at the top left if it is not selected yet.
2. Pick a recent time range, such as the last 15 minutes, and click
   **Refresh**. Turn on **Live** to tail new records as they come in.
3. Click a row to open its details: body, attributes and resource.

Nothing showing up? Open **Build**, **Pipelines**, open the pipeline and
check it is running and its counters show requests. A wrong key shows up as
rejections labelled `bad key`.

## Other ways in

[Ingest](/docs/ingest) covers the rest: JSON events into any table, the
Elasticsearch bulk API for Filebeat and Logstash, Splunk HEC, Vector, key
limits and the separate ingest port. To read tables an existing collector
already writes, see [Telemetry sources](/docs/telemetry#sources).
