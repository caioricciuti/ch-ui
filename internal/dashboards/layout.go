package dashboards

import (
	"fmt"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// Grid bounds, matching ui/src/lib/utils/grid-layout.ts: 12 columns, and a
// generous cap on rows so a typo cannot push a panel out of reach.
const (
	GridCols    = 12
	GridMaxRow  = 10000
	GridMaxRows = 100 // tallest panel, in rows
)

// NextPanelRow is the first grid row below every panel: max(y+h), 0 when
// there are none, capped so the result still passes ValidateLayout.
func NextPanelRow(panels []database.Panel) int {
	row := 0
	for _, p := range panels {
		if bottom := p.LayoutY + p.LayoutH; bottom > row {
			row = bottom
		}
	}
	if row > GridMaxRow {
		row = GridMaxRow
	}
	return row
}

// ValidateLayout returns an error message when a panel does not fit the grid,
// or "" when it does.
func ValidateLayout(x, y, w, h int) string {
	switch {
	case w < 1 || w > GridCols:
		return fmt.Sprintf("layout_w must be between 1 and %d", GridCols)
	case x < 0 || x+w > GridCols:
		return fmt.Sprintf("layout_x must be at least 0 and layout_x + layout_w at most %d", GridCols)
	case h < 1 || h > GridMaxRows:
		return fmt.Sprintf("layout_h must be between 1 and %d", GridMaxRows)
	case y < 0 || y > GridMaxRow:
		return fmt.Sprintf("layout_y must be between 0 and %d", GridMaxRow)
	}
	return ""
}

// ClampLayout moves and resizes a panel into the grid, so the result always
// passes ValidateLayout: the width first, then x so the panel ends inside the
// last column, then the height and the row.
func ClampLayout(x, y, w, h int) (int, int, int, int) {
	w = clampInt(w, 1, GridCols)
	x = clampInt(x, 0, GridCols-w)
	h = clampInt(h, 1, GridMaxRows)
	y = clampInt(y, 0, GridMaxRow)
	return x, y, w, h
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
