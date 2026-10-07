import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  lastDays, mergeDaily, sumBuckets, successRate, averageDuration, totalTokens,
  formatCredits, formatDuration, emptyTotals
} from '../pkg/agentic/utils/stats.ts';

const NOW = new Date('2026-10-07T15:30:00Z');

test('lastDays returns UTC day keys ending today, oldest first', () => {
  assert.deepEqual(lastDays(3, NOW), ['2026-10-05', '2026-10-06', '2026-10-07']);
});

test('mergeDaily sums agents per day and zero-fills missing days', () => {
  const a = [{
    date: '2026-10-06', runs: 2, succeeded: 1, failed: 1, aiCredits: 10.5
  }];
  const b = [
    {
      date: '2026-10-06', runs: 1, succeeded: 1, aiCredits: 2
    },
    {
      date: '2026-10-07', runs: 3, succeeded: 3, pullRequests: 1
    },
  ];
  const merged = mergeDaily([a, b, undefined], 3, NOW);

  assert.equal(merged.length, 3);
  assert.equal(merged[0].date, '2026-10-05');
  assert.equal(merged[0].runs, 0);
  assert.equal(merged[1].runs, 3);
  assert.equal(merged[1].aiCredits, 12.5);
  assert.equal(merged[2].pullRequests, 1);
});

test('mergeDaily drops buckets outside the range', () => {
  const merged = mergeDaily([[{ date: '2026-09-01', runs: 9 }]], 7, NOW);

  assert.equal(sumBuckets(merged).runs, 0);
});

test('successRate ignores cancelled runs and handles no finished runs', () => {
  assert.equal(successRate({
    succeeded: 3, failed: 1, cancelled: 5
  }), 75);
  assert.equal(successRate(emptyTotals()), null);
});

test('averageDuration and totalTokens', () => {
  assert.equal(averageDuration({ runs: 4, durationSeconds: 1000 }), 250);
  assert.equal(averageDuration({ runs: 0 }), null);
  assert.equal(totalTokens({
    inputTokens: 1, outputTokens: 2, cacheReadTokens: 3, cacheWriteTokens: 4
  }), 10);
});

test('formatters', () => {
  assert.equal(formatCredits(96.56567), '96.6');
  assert.equal(formatCredits(1234.4), '1,234');
  assert.equal(formatCredits(undefined), '—');
  assert.equal(formatDuration(45), '45s');
  assert.equal(formatDuration(125), '2m 5s');
  assert.equal(formatDuration(3720), '1h 2m');
});
