import React from "react";
import {
  DndContext,
  closestCenter,
  PointerSensor,
  useSensor,
  useSensors,
  DragEndEvent,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  useSortable,
  rectSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { cn } from "@/lib/utils";
import { Grip } from "lucide-react";

type GridItem = {
  id: string;
  colSpan: number; // 1..12
  rowSpan: number; // number of auto-rows to span
};

type ItemLike = { title: string; type?: string; tiles?: number };

function defaultSizeFor(it: ItemLike): GridItem {
  // Defaults tuned per type
  const typeDefaultCols = it.type === 'table' ? 12 : it.type === 'card' ? 3 : 12;
  const col = Math.max(2, Math.min(12, it.tiles ? it.tiles * 3 : typeDefaultCols));
  // Use fractional units instead of fixed pixel rows
  const rowSpan = it.type === 'table' ? 4 : it.type === 'card' ? 1 : 4; // cards = 1 row, charts = 4 rows
  return { id: it.title, colSpan: col, rowSpan };
}

function minSizeFor(it: ItemLike): GridItem {
  // Allow narrow stats; charts/tables prefer wider
  const minCols = it.type === 'table' ? 6 : it.type === 'card' ? 2 : 4;
  const minRows = it.type === 'table' ? 3 : it.type === 'card' ? 1 : 3; // charts need at least 3 rows
  return { id: it.title, colSpan: minCols, rowSpan: minRows };
}

function usePersistedLayout(scope: string, items: ItemLike[]) {
  const key = `metrics_layout_${scope}`;
  const versionKey = `${key}_version`;
  const layoutVersion = 14; // updated for 4-row charts
  const initial = React.useMemo(() => {
    const raw = localStorage.getItem(key);
    const ver = Number(localStorage.getItem(versionKey) || '0');
    if (raw && ver === layoutVersion) {
      try {
        return JSON.parse(raw) as { order: string[]; sizes: Record<string, GridItem> };
      } catch {}
    }
    const order = items.map(i => i.title);
    const sizes: Record<string, GridItem> = Object.fromEntries(
      items.map(i => [i.title, defaultSizeFor(i)])
    );
    return { order, sizes };
  }, [key, items]);

  const [state, setState] = React.useState(initial);
  const save = React.useCallback((next: typeof state) => {
    setState(next);
    localStorage.setItem(key, JSON.stringify(next));
    localStorage.setItem(versionKey, String(layoutVersion));
  }, [key]);

  // ensure new items get defaults
  React.useEffect(() => {
    const missing = items.filter(i => !state.sizes[i.title]);
    if (missing.length) {
      const sizes = { ...state.sizes };
      missing.forEach(i => sizes[i.title] = defaultSizeFor(i));
      const order = Array.from(new Set([...state.order, ...items.map(i => i.title)]));
      save({ order, sizes });
    }
  }, [items]);

  return { state, save };
}

type SortableRenderArgs = {
  setNodeRef: (el: HTMLElement | null) => void;
  style: React.CSSProperties;
  attributes: any;
  listeners: any;
};
function SortableItem({ id, children }: { id: string; children: (args: SortableRenderArgs) => React.ReactNode }) {
  const { attributes, listeners, setNodeRef, transform, transition } = useSortable({ id });
  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    willChange: 'transform',
  };
  return (
    <>{children({ setNodeRef, style, attributes, listeners })}</>
  );
}

export default function DashboardGrid<T extends ItemLike>({
  scope,
  items,
  renderItem,
}: {
  scope: string;
  items: T[];
  renderItem: (item: T) => React.ReactNode;
}) {
  const { state, save } = usePersistedLayout(scope, items);
  const order = state.order.filter(id => items.find(i => i.title === id));
  const byId = Object.fromEntries(items.map(i => [i.title, i]));
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }));

  const onDragEnd = (e: DragEndEvent) => {
    const { active, over } = e;
    if (!over || active.id === over.id) return;
    const oldIndex = order.indexOf(String(active.id));
    const newIndex = order.indexOf(String(over.id));
    const nextOrder = arrayMove(order, oldIndex, newIndex);
    save({ ...state, order: nextOrder });
  };

  const bump = (id: string, dx: number, dy: number) => {
    const sizes = { ...state.sizes };
    const cur = sizes[id];
    const itm = byId[id];
    const min = minSizeFor(itm);
    sizes[id] = {
      ...cur,
      colSpan: Math.min(12, Math.max(min.colSpan, cur.colSpan + dx * 1)), // step 1 column
      rowSpan: Math.min(12, Math.max(min.rowSpan, cur.rowSpan + dy * 1)), // step 1 flexible row
    };
    save({ ...state, sizes });
  };

  const resetLayout = () => {
    const order = items.map(i => i.title);
    const sizes: Record<string, GridItem> = Object.fromEntries(
      items.map(i => [i.title, defaultSizeFor(i)])
    );
    save({ order, sizes });
  };

  const startResize = (
    e: React.PointerEvent,
    id: string,
  ) => {
    e.stopPropagation();
    const handleEl = e.currentTarget as HTMLElement;
    (handleEl as any).setPointerCapture?.(e.pointerId);
    const tileEl = handleEl.closest('[data-grid-tile]') as HTMLElement | null;
    const gridEl = tileEl?.parentElement as HTMLElement | null;
    if (!tileEl || !gridEl) return;

    const startX = e.clientX;
    const startY = e.clientY;
    const rect = gridEl.getBoundingClientRect();
    const styles = window.getComputedStyle(gridEl);
    const colGap = parseFloat(styles.columnGap || '0') || 0;
    const totalCols = 12;
    const colWidth = (rect.width - colGap * (totalCols - 1)) / totalCols;
    // For flexible grid, use a reasonable row height for resize calculations
    const rowHeight = 100; // Match gridAutoRows minmax(100px, auto)

    const sizes = { ...state.sizes };
    const cur = sizes[id];
    const itm = byId[id];
    const min = minSizeFor(itm);

    const onMove = (ev: PointerEvent) => {
      const dx = ev.clientX - startX;
      const dy = ev.clientY - startY;
      const dCols = Math.round(dx / colWidth);
      const dRows = Math.round(dy / rowHeight);
      const next = {
        ...cur,
        colSpan: Math.max(min.colSpan, Math.min(12, cur.colSpan + dCols)),
        rowSpan: Math.max(min.rowSpan, Math.min(12, cur.rowSpan + dRows)),
      };
      sizes[id] = next;
      save({ ...state, sizes });
    };

    const onUp = () => {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
    };

    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
  };

  const startResizeX = (
    e: React.PointerEvent,
    id: string,
  ) => {
    e.stopPropagation();
    const handleEl = e.currentTarget as HTMLElement;
    (handleEl as any).setPointerCapture?.(e.pointerId);
    const tileEl = handleEl.closest('[data-grid-tile]') as HTMLElement | null;
    const gridEl = tileEl?.parentElement as HTMLElement | null;
    if (!tileEl || !gridEl) return;

    const startX = e.clientX;
    const rect = gridEl.getBoundingClientRect();
    const styles = window.getComputedStyle(gridEl);
    const colGap = parseFloat(styles.columnGap || '0') || 0;
    const totalCols = 12;
    const colWidth = (rect.width - colGap * (totalCols - 1)) / totalCols;

    const sizes = { ...state.sizes };
    const cur = sizes[id];
    const itm = byId[id];
    const min = minSizeFor(itm);

    const onMove = (ev: PointerEvent) => {
      const dx = ev.clientX - startX;
      const dCols = Math.round(dx / colWidth);
      const next = {
        ...cur,
        colSpan: Math.max(min.colSpan, Math.min(12, cur.colSpan + dCols)),
      };
      sizes[id] = next as GridItem;
      save({ ...state, sizes });
    };

    const onUp = () => {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
    };

    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
  };

  const startResizeY = (
    e: React.PointerEvent,
    id: string,
  ) => {
    e.stopPropagation();
    const handleEl = e.currentTarget as HTMLElement;
    (handleEl as any).setPointerCapture?.(e.pointerId);
    const tileEl = handleEl.closest('[data-grid-tile]') as HTMLElement | null;
    const gridEl = tileEl?.parentElement as HTMLElement | null;
    if (!tileEl || !gridEl) return;

    const startY = e.clientY;
    const rect = gridEl.getBoundingClientRect();
    const rowHeight = 100; // Match gridAutoRows minmax(100px, auto)

    const sizes = { ...state.sizes };
    const cur = sizes[id];
    const itm = byId[id];
    const min = minSizeFor(itm);

    const onMove = (ev: PointerEvent) => {
      const dy = ev.clientY - startY;
      const dRows = Math.round(dy / rowHeight);
      const next = {
        ...cur,
        rowSpan: Math.max(min.rowSpan, Math.min(12, cur.rowSpan + dRows)),
      };
      sizes[id] = next as GridItem;
      save({ ...state, sizes });
    };

    const onUp = () => {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
    };

    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
  };

  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
      <SortableContext items={order} strategy={rectSortingStrategy}>
        <div className="flex items-center justify-end pb-2">
          <button className="text-xs px-2 py-1 rounded border hover:bg-accent" onClick={resetLayout}>Reset layout</button>
        </div>
        <div
          className="grid grid-cols-12 gap-4"
          style={{
            gridAutoRows: 'minmax(100px, auto)',
            minHeight: '400px'
          }}
        >
          {order.map((id) => {
            const itm = byId[id];
            if (!itm) return null;
            const sizeRaw = state.sizes[id] || defaultSizeFor(itm);
            const min = minSizeFor(itm);
            const size = {
              id,
              colSpan: Math.max(min.colSpan, Math.min(12, sizeRaw.colSpan)),
              rowSpan: Math.max(min.rowSpan, Math.min(12, sizeRaw.rowSpan)),
            };
            return (
              <SortableItem key={id} id={id}>
                {({ setNodeRef, style, attributes, listeners }) => {
                  const mergedStyle: React.CSSProperties = {
                    ...(style || {}),
                    gridColumn: `span ${size.colSpan} / span ${size.colSpan}`,
                    gridRow: `span ${size.rowSpan}`,
                  };
                  return (
                  <div
                    ref={setNodeRef}
                    style={mergedStyle}
                    className="relative h-full group"
                    data-grid-tile
                  >
                    {/* Move grip (drag handle only) */}
                    <button
                      className="absolute left-2 top-2 z-10 p-1 rounded bg-background/80 border cursor-grab active:cursor-grabbing opacity-0 group-hover:opacity-100 transition-opacity"
                      title="Drag to move"
                      {...attributes}
                      {...listeners}
                    >
                      <Grip className="h-4 w-4" />
                    </button>
                    {/* Quick size controls removed for cleaner UI */}
                    {/* Drag resize handles */}
                    <div
                      onPointerDown={(e) => startResizeX(e, id)}
                      className="absolute -right-1 top-1/2 -translate-y-1/2 z-10 h-6 w-2 rounded-sm border bg-background/90 shadow cursor-e-resize opacity-0 group-hover:opacity-100 transition-opacity"
                      title="Drag to resize width"
                    />
                    <div
                      onPointerDown={(e) => startResizeY(e, id)}
                      className="absolute left-1/2 -translate-x-1/2 -bottom-1 z-10 h-2 w-6 rounded-sm border bg-background/90 shadow cursor-s-resize opacity-0 group-hover:opacity-100 transition-opacity"
                      title="Drag to resize height"
                    />
                    <div
                      onPointerDown={(e) => startResize(e, id)}
                      className="absolute right-1 bottom-1 z-10 h-5 w-5 rounded-sm border bg-background/90 shadow cursor-se-resize opacity-0 group-hover:opacity-100 transition-opacity"
                      title="Drag to resize"
                    />
                    {renderItem(itm)}
                  </div>
                  );
                }}
              </SortableItem>
            );
          })}
        </div>
      </SortableContext>
    </DndContext>
  );
}
