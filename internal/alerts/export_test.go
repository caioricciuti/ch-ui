// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import "github.com/caioricciuti/ch-ui/internal/database"

// MaterializeForTest turns new events into dispatch jobs, for the external
// test package.
func MaterializeForTest(db *database.DB) {
	(&Dispatcher{db: db}).materializeEventJobs()
}
