<script setup lang="ts">
// Tiles + the two trend charts, shared by the overview page and the agent detail page
import { computed } from 'vue';
import { useStore } from 'vuex';
import StatTile from './StatTile.vue';
import DailyColumns, { Series } from './DailyColumns.vue';
import {
  DailyBucket, mergeDaily, sumBuckets, successRate, averageDuration, totalTokens,
  formatCredits, formatCompact, formatDuration
} from '../utils/stats';

type DailyLists = (DailyBucket[] | undefined)[];

const props = withDefaults(defineProps<{
  dailyLists: DailyLists;
  days: number;
  agentCount?: number | null;
}>(), { agentCount: null });

const store = useStore();
const t = store.getters['i18n/t'];

const buckets = computed(() => mergeDaily(props.dailyLists, props.days));
const totals = computed(() => sumBuckets(buckets.value));
const rate = computed(() => successRate(totals.value));

const runSeries = computed<Series[]>(() => [
  {
    key: 'succeeded', label: t('agentic.stats.succeeded'), color: '--agentic-viz-ok'
  },
  {
    key: 'failed', label: t('agentic.stats.failed'), color: '--agentic-viz-fail'
  },
]);

const creditSeries = computed<Series[]>(() => [
  {
    key: 'aiCredits', label: t('agentic.stats.aiCredits'), color: '--agentic-viz-ok'
  },
]);

const runExtra = (b: DailyBucket) => (b.cancelled ? [t('agentic.stats.cancelledCount', { n: b.cancelled })] : []);
const creditExtra = (b: DailyBucket) => (b.runs ? [t('agentic.stats.perRun', { n: formatCredits((b.aiCredits || 0) / b.runs) })] : []);
</script>

<template>
  <div class="agent-stats">
    <div class="tiles">
      <StatTile
        v-if="agentCount !== null"
        :label="t('agentic.stats.agents')"
        :value="String(agentCount)"
      />
      <StatTile
        :label="t('agentic.stats.runs')"
        :value="totals.runs.toLocaleString()"
        :detail="t('agentic.stats.runsDetail', { failed: totals.failed, cancelled: totals.cancelled })"
      />
      <StatTile
        :label="t('agentic.stats.successRate')"
        :value="rate === null ? '—' : `${ rate }%`"
        :detail="t('agentic.stats.avgDuration', { d: formatDuration(averageDuration(totals)) })"
      />
      <StatTile
        :label="t('agentic.stats.aiCredits')"
        :value="formatCredits(totals.aiCredits)"
        :detail="t('agentic.stats.tokens', { n: formatCompact(totalTokens(totals)) })"
      />
      <StatTile
        :label="t('agentic.stats.pullRequests')"
        :value="totals.pullRequests.toLocaleString()"
        :detail="t('agentic.stats.prDetail', { merged: totals.pullRequestsMerged, closed: totals.pullRequestsClosed })"
      />
      <StatTile
        :label="t('agentic.stats.otherOutputs')"
        :value="(totals.comments + totals.issues + totals.labels).toLocaleString()"
        :detail="t('agentic.stats.otherDetail', { comments: totals.comments, issues: totals.issues, labels: totals.labels })"
      />
    </div>

    <div class="charts">
      <DailyColumns
        :title="t('agentic.chart.runsTitle')"
        :subtitle="t('agentic.chart.runsSubtitle', { days })"
        :buckets="buckets"
        :series="runSeries"
        :extra="runExtra"
      />
      <DailyColumns
        :title="t('agentic.chart.creditsTitle')"
        :subtitle="t('agentic.chart.creditsSubtitle', { days })"
        :buckets="buckets"
        :series="creditSeries"
        :format="formatCredits"
        :extra="creditExtra"
      />
    </div>
  </div>
</template>

<style lang="scss" scoped>
.agent-stats {
  // Validated with the dataviz palette checker against light and dark surfaces
  --agentic-viz-ok: #2F68DF;
  --agentic-viz-fail: #B13333;

  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 12px;
    margin-bottom: 24px;
  }

  .charts {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(380px, 1fr));
    gap: 32px;
  }
}

</style>

<style lang="scss">
// Unscoped so it can key off the theme class on <body>
.theme-dark .agent-stats {
  --agentic-viz-ok: #4F86F0;
  --agentic-viz-fail: #E05252;
}
</style>
