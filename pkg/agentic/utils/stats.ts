// Helpers over Agent.status.daily buckets. Kept free of Vue/store code so they can be unit tested.

export interface DailyBucket {
  date: string;
  runs?: number;
  succeeded?: number;
  failed?: number;
  cancelled?: number;
  durationSeconds?: number;
  aiCredits?: number;
  inputTokens?: number;
  outputTokens?: number;
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
  comments?: number;
  issues?: number;
  pullRequests?: number;
  pullRequestsMerged?: number;
  pullRequestsClosed?: number;
  labels?: number;
  otherOutputs?: number;
}

export type Totals = Required<Omit<DailyBucket, 'date'>>;

const NUMERIC_KEYS: (keyof Totals)[] = [
  'runs', 'succeeded', 'failed', 'cancelled', 'durationSeconds', 'aiCredits',
  'inputTokens', 'outputTokens', 'cacheReadTokens', 'cacheWriteTokens',
  'comments', 'issues', 'pullRequests', 'pullRequestsMerged', 'pullRequestsClosed', 'labels', 'otherOutputs',
];

export function emptyTotals(): Totals {
  return NUMERIC_KEYS.reduce((acc, k) => ({ ...acc, [k]: 0 }), {} as Totals);
}

export function addInto(target: Totals, bucket: Partial<DailyBucket>): Totals {
  NUMERIC_KEYS.forEach((k) => {
    target[k] += Number(bucket[k] || 0);
  });

  return target;
}

/** UTC YYYY-MM-DD for a Date */
export function dayKey(d: Date): string {
  return d.toISOString().slice(0, 10);
}

/** The last `days` UTC day keys, oldest first, ending today */
export function lastDays(days: number, now: Date = new Date()): string[] {
  const out: string[] = [];
  const end = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());

  for (let i = days - 1; i >= 0; i--) {
    out.push(dayKey(new Date(end - i * 86400000)));
  }

  return out;
}

/**
 * Merge the daily buckets of many agents into one series covering the last `days` days.
 * Days without data are present with zeros so charts keep an even time axis.
 */
export function mergeDaily(bucketLists: (DailyBucket[] | undefined)[], days: number, now: Date = new Date()): DailyBucket[] {
  const keys = lastDays(days, now);
  const byDay = new Map<string, Totals>(keys.map((k) => [k, emptyTotals()]));

  bucketLists.forEach((list) => {
    (list || []).forEach((b) => {
      const t = byDay.get(b.date);

      if (t) {
        addInto(t, b);
      }
    });
  });

  return keys.map((date) => ({ date, ...byDay.get(date) as Totals }));
}

export function sumBuckets(buckets: DailyBucket[]): Totals {
  return buckets.reduce((acc, b) => addInto(acc, b), emptyTotals());
}

/** Success rate in percent over finished runs, or null when nothing finished */
export function successRate(t: Partial<Totals>): number | null {
  const finished = (t.succeeded || 0) + (t.failed || 0);

  return finished ? Math.round(((t.succeeded || 0) / finished) * 100) : null;
}

export function averageDuration(t: Partial<Totals>): number | null {
  return t.runs ? Math.round((t.durationSeconds || 0) / t.runs) : null;
}

export function totalTokens(t: Partial<Totals>): number {
  return (t.inputTokens || 0) + (t.outputTokens || 0) + (t.cacheReadTokens || 0) + (t.cacheWriteTokens || 0);
}

export function formatCredits(n?: number | null): string {
  if (n === undefined || n === null) {
    return '—';
  }

  return n >= 100 ? Math.round(n).toLocaleString() : n.toFixed(1);
}

export function formatCompact(n?: number | null): string {
  if (n === undefined || n === null) {
    return '—';
  }

  return new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }).format(n);
}

export function formatDuration(seconds?: number | null): string {
  if (seconds === undefined || seconds === null) {
    return '—';
  }
  if (seconds < 60) {
    return `${ seconds }s`;
  }
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;

  if (m < 60) {
    return s ? `${ m }m ${ s }s` : `${ m }m`;
  }

  return `${ Math.floor(m / 60) }h ${ m % 60 }m`;
}
