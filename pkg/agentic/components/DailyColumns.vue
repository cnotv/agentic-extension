<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { DailyBucket } from '../utils/stats';

export interface Series {
  key: keyof DailyBucket;
  label: string;
  // CSS custom property holding the series colour, e.g. --agentic-viz-ok
  color: string;
}

const props = withDefaults(defineProps<{
  title: string;
  subtitle?: string;
  buckets: DailyBucket[];
  series: Series[];
  format?:(n: number) => string;
  // Extra lines shown in the tooltip, e.g. cancelled runs that are not drawn
  extra?: (b: DailyBucket) => string[];
  height?: number;
}>(), {
  subtitle: '',
  format:   (n: number) => n.toLocaleString(),
  extra:    () => [],
  height:   180,
});

const PAD = {
  top: 8, right: 8, bottom: 24, left: 44
};
const GAP = 2;
const MAX_BAR = 24;

const root = ref<HTMLElement | null>(null);
const width = ref(600);
const hover = ref<number | null>(null);
let observer: ResizeObserver | undefined;

onMounted(() => {
  if (root.value) {
    width.value = root.value.clientWidth || 600;
    observer = new ResizeObserver(([entry]) => {
      width.value = Math.max(240, Math.floor(entry.contentRect.width));
    });
    observer.observe(root.value);
  }
});

onBeforeUnmount(() => observer?.disconnect());

const plotW = computed(() => width.value - PAD.left - PAD.right);
const plotH = computed(() => props.height - PAD.top - PAD.bottom);

const totals = computed(() => props.buckets.map((b) => props.series.reduce((sum, s) => sum + Number(b[s.key] || 0), 0)));

// Round the axis top up to a clean 1/2/5 step so ticks read as plain numbers
const axis = computed(() => {
  const max = Math.max(0, ...totals.value);

  if (max === 0) {
    return { top: 1, ticks: [0, 1] };
  }
  const rough = max / 4;
  const mag = 10 ** Math.floor(Math.log10(rough));
  let step = [1, 2, 5, 10].map((m) => m * mag).find((s) => s >= rough) as number;

  // Counts never need fractional ticks
  if (totals.value.every((v) => Number.isInteger(v))) {
    step = Math.max(1, step);
  }
  const top = Math.ceil(max / step) * step;
  const ticks = [];

  for (let v = 0; v <= top + step / 2; v += step) {
    ticks.push(Number(v.toPrecision(6)));
  }

  return { top, ticks };
});

const band = computed(() => plotW.value / Math.max(1, props.buckets.length));
const barW = computed(() => Math.max(2, Math.min(MAX_BAR, band.value - GAP)));

// Whole-number ticks read cleaner than the value format (e.g. 50 not 50.0)
const tickLabel = (v: number) => (Number.isInteger(v) ? v.toLocaleString() : props.format(v));

const y = (v: number) => PAD.top + plotH.value - (v / axis.value.top) * plotH.value;

// One rect per non-zero segment; the top segment gets the rounded data-end
const columns = computed(() => props.buckets.map((b, i) => {
  const x = PAD.left + i * band.value + (band.value - barW.value) / 2;
  let base = 0;
  const segments = props.series
    .map((s) => ({ s, v: Number(b[s.key] || 0) }))
    .filter(({ v }) => v > 0)
    .map(({ s, v }) => {
      const y0 = y(base);
      const y1 = y(base + v);

      base += v;

      return {
        key: s.key, color: s.color, x, y: y1, h: Math.max(0, y0 - y1)
      };
    });

  // Leave the surface gap between stacked segments
  segments.forEach((seg, idx) => {
    if (idx > 0) {
      seg.h = Math.max(0, seg.h - GAP);
    }
  });

  return {
    x, segments, top: segments[segments.length - 1]
  };
}));

function columnPath(seg: { x: number, y: number, h: number }, rounded: boolean) {
  const w = barW.value;
  const r = rounded ? Math.min(4, w / 2, seg.h) : 0;
  const { x, y: top, h } = seg;

  return `M${ x },${ top + h }V${ top + r }Q${ x },${ top } ${ x + r },${ top }H${ x + w - r }Q${ x + w },${ top } ${ x + w },${ top + r }V${ top + h }Z`;
}

const xLabels = computed(() => {
  const n = props.buckets.length;

  if (!n) {
    return [];
  }
  const idx = n > 2 ? [0, Math.floor((n - 1) / 2), n - 1] : [...Array(n).keys()];

  return idx.map((i) => ({
    x:     PAD.left + i * band.value + band.value / 2,
    label: shortDate(props.buckets[i].date),
    i,
  }));
});

function shortDate(d: string) {
  return new Date(`${ d }T00:00:00Z`).toLocaleDateString(undefined, {
    month: 'short', day: 'numeric', timeZone: 'UTC'
  });
}

const tooltip = computed(() => {
  if (hover.value === null) {
    return null;
  }
  const b = props.buckets[hover.value];
  const center = PAD.left + hover.value * band.value + band.value / 2;

  return {
    left: Math.min(Math.max(center, 80), width.value - 80),
    date: shortDate(b.date),
    rows: props.series.map((s) => ({
      label: s.label, color: s.color, value: props.format(Number(b[s.key] || 0))
    })),
    extra: props.extra(b),
  };
});

const isEmpty = computed(() => totals.value.every((t) => t === 0));
</script>

<template>
  <figure
    ref="root"
    class="daily-columns"
  >
    <figcaption>
      <h3 class="title">
        {{ title }}
      </h3>
      <p
        v-if="subtitle"
        class="subtitle text-muted"
      >
        {{ subtitle }}
      </p>
      <ul
        v-if="series.length > 1"
        class="legend"
      >
        <li
          v-for="s in series"
          :key="s.key"
        >
          <span
            class="swatch"
            :style="{ background: `var(${ s.color })` }"
          />{{ s.label }}
        </li>
      </ul>
    </figcaption>

    <div class="plot">
      <svg
        :width="width"
        :height="height"
        role="img"
        :aria-label="title"
        @mouseleave="hover = null"
      >
        <g class="grid">
          <template
            v-for="t in axis.ticks"
            :key="t"
          >
            <line
              :x1="PAD.left"
              :x2="width - PAD.right"
              :y1="y(t)"
              :y2="y(t)"
            />
            <text
              :x="PAD.left - 6"
              :y="y(t)"
              text-anchor="end"
              dominant-baseline="middle"
            >{{ tickLabel(t) }}</text>
          </template>
        </g>

        <g
          v-for="(col, i) in columns"
          :key="i"
          :class="{ dim: hover !== null && hover !== i }"
        >
          <path
            v-for="seg in col.segments"
            :key="seg.key"
            :d="columnPath(seg, seg === col.top)"
            :style="{ fill: `var(${ seg.color })` }"
          />
          <!-- Full-height hit target, wider than the bar -->
          <rect
            class="hit"
            :x="PAD.left + i * band"
            :y="PAD.top"
            :width="band"
            :height="plotH"
            @mouseenter="hover = i"
          />
        </g>

        <g class="x-axis">
          <text
            v-for="l in xLabels"
            :key="l.i"
            :x="l.x"
            :y="height - 6"
            :text-anchor="l.i === 0 ? 'start' : (l.i === buckets.length - 1 ? 'end' : 'middle')"
          >{{ l.label }}</text>
        </g>
      </svg>

      <p
        v-if="isEmpty"
        class="empty text-muted"
      >
        {{ t('agentic.chart.empty') }}
      </p>

      <div
        v-if="tooltip"
        class="tooltip"
        :style="{ left: `${ tooltip.left }px` }"
      >
        <div class="tooltip-date">
          {{ tooltip.date }}
        </div>
        <div
          v-for="row in tooltip.rows"
          :key="row.label"
          class="tooltip-row"
        >
          <span
            class="swatch"
            :style="{ background: `var(${ row.color })` }"
          />
          <span>{{ row.label }}</span>
          <span class="value">{{ row.value }}</span>
        </div>
        <div
          v-for="line in tooltip.extra"
          :key="line"
          class="tooltip-row text-muted"
        >
          {{ line }}
        </div>
      </div>
    </div>
  </figure>
</template>

<style lang="scss" scoped>
.daily-columns {
  margin: 0;
  min-width: 0;

  figcaption {
    margin-bottom: 8px;
  }

  .title {
    margin: 0;
    font-size: 15px;
  }

  .subtitle {
    margin: 2px 0 0;
    font-size: 12px;
  }

  .legend {
    display: flex;
    gap: 16px;
    list-style: none;
    margin: 6px 0 0;
    padding: 0;
    font-size: 12px;
    color: var(--body-text);

    li {
      display: flex;
      align-items: center;
    }
  }

  .swatch {
    display: inline-block;
    width: 10px;
    height: 10px;
    border-radius: 2px;
    margin-right: 6px;
    flex-shrink: 0;
  }

  .plot {
    position: relative;
  }

  svg {
    display: block;
    overflow: visible;
  }

  .grid line {
    stroke: var(--border);
    stroke-width: 1;
  }

  .grid text, .x-axis text {
    fill: var(--muted);
    font-size: 11px;
  }

  .dim path {
    opacity: 0.45;
  }

  .hit {
    fill: transparent;
  }

  .empty {
    position: absolute;
    inset: 0 0 24px 44px;
    display: flex;
    align-items: center;
    justify-content: center;
    margin: 0;
  }

  .tooltip {
    position: absolute;
    top: 0;
    transform: translateX(-50%);
    pointer-events: none;
    background: var(--body-bg);
    border: 1px solid var(--border);
    border-radius: var(--border-radius);
    box-shadow: 0 2px 8px var(--shadow);
    padding: 8px 10px;
    font-size: 12px;
    min-width: 140px;
    z-index: 2;
  }

  .tooltip-date {
    font-weight: 600;
    margin-bottom: 4px;
  }

  .tooltip-row {
    display: flex;
    align-items: center;
    gap: 2px;

    .value {
      margin-left: auto;
      padding-left: 12px;
      font-variant-numeric: tabular-nums;
    }
  }
}
</style>
