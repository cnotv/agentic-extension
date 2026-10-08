<script>
import { mapGetters } from 'vuex';
import Loading from '@shell/components/Loading';
import ButtonGroup from '@shell/components/ButtonGroup';
import SortableTable from '@shell/components/SortableTable';
import ResourceTable from '@shell/components/ResourceTable';
import { Banner } from '@components/Banner';
import AgentStats from '../components/AgentStats.vue';
import { AGENTIC, PRODUCT_NAME, BLANK_CLUSTER } from '../config/types';
import { mergeDaily, sumBuckets, successRate, formatCredits } from '../utils/stats';

const RECENT_RUNS = 10;

export default {
  name: 'AgenticOverview',

  components: {
    Loading, ButtonGroup, SortableTable, ResourceTable, Banner, AgentStats
  },

  async fetch() {
    const hash = await Promise.all([
      this.$store.dispatch('management/findAll', { type: AGENTIC.REPOSITORY }),
      this.$store.dispatch('management/findAll', { type: AGENTIC.AGENT }),
      this.$store.dispatch('management/findAll', { type: AGENTIC.RUN }),
    ]);

    [this.repositories, this.agents, this.runs] = hash;
  },

  data() {
    return {
      repositories: [],
      agents:       [],
      runs:         [],
      days:         30,
      dayOptions:   [7, 30, 90].map((d) => ({ label: this.t('agentic.overview.days', { d }), value: d })),
    };
  },

  computed: {
    ...mapGetters(['currentCluster']),

    hasSchema() {
      return !!this.$store.getters['management/schemaFor'](AGENTIC.AGENT);
    },

    runSchema() {
      return this.$store.getters['management/schemaFor'](AGENTIC.RUN);
    },

    dailyLists() {
      return this.agents.map((a) => a.daily);
    },

    erroredRepositories() {
      return this.repositories.filter((r) => r.status?.phase === 'Error');
    },

    createRepositoryLocation() {
      return {
        name:   `${ PRODUCT_NAME }-c-cluster-resource-create`,
        params: {
          product: PRODUCT_NAME, cluster: BLANK_CLUSTER, resource: AGENTIC.REPOSITORY
        }
      };
    },

    agentHeaders() {
      return [
        {
          name: 'agent', labelKey: 'agentic.headers.agent', value: 'nameDisplay', sort: ['nameDisplay']
        },
        {
          name: 'repository', labelKey: 'agentic.headers.repository', value: 'repository', sort: ['repository']
        },
        {
          name: 'runs', labelKey: 'agentic.headers.runs', value: 'runs', sort: ['runs'], align: 'right'
        },
        {
          name: 'successRate', labelKey: 'agentic.headers.successRate', value: 'successRateDisplay', sort: ['successRateSort'], align: 'right'
        },
        {
          name: 'aiCredits', labelKey: 'agentic.headers.aiCredits', value: 'aiCreditsDisplay', sort: ['aiCredits'], align: 'right'
        },
        {
          name: 'pullRequests', labelKey: 'agentic.headers.pullRequests', value: 'pullRequests', sort: ['pullRequests'], align: 'right'
        },
        {
          name: 'lastRun', labelKey: 'agentic.headers.lastRun', value: 'lastRun', sort: ['lastRun'], formatter: 'LiveDate'
        },
      ];
    },

    // Per-agent totals within the selected range, most expensive first
    agentRows() {
      return this.agents.map((agent) => {
        const totals = sumBuckets(mergeDaily([agent.daily], this.days));
        const rate = successRate(totals);

        return {
          id:                 agent.id,
          agent,
          nameDisplay:        agent.nameDisplay,
          repository:         agent.spec?.repository,
          runs:               totals.runs,
          successRateDisplay: rate === null ? '—' : `${ rate }%`,
          successRateSort:    rate === null ? -1 : rate,
          aiCredits:          totals.aiCredits,
          aiCreditsDisplay:   formatCredits(totals.aiCredits),
          pullRequests:       totals.pullRequests,
          lastRun:            agent.lastRunTime,
        };
      }).sort((a, b) => b.aiCredits - a.aiCredits);
    },

    recentRuns() {
      return [...this.runs]
        .sort((a, b) => `${ b.startTime || '' }`.localeCompare(`${ a.startTime || '' }`))
        .slice(0, RECENT_RUNS);
    },
  },
};
</script>

<template>
  <Loading v-if="$fetchState.pending" />
  <div
    v-else
    class="agentic-overview"
  >
    <header class="page-header">
      <div>
        <h1>{{ t('agentic.overview.title') }}</h1>
        <p class="text-muted">
          {{ t('agentic.overview.subtitle') }}
        </p>
      </div>
      <ButtonGroup
        v-if="agents.length"
        v-model:value="days"
        :options="dayOptions"
      />
    </header>

    <Banner
      v-if="!hasSchema"
      color="warning"
      :label="t('agentic.overview.noController')"
    />

    <Banner
      v-for="repo in erroredRepositories"
      :key="repo.id"
      color="error"
    >
      <router-link :to="repo.detailLocation">
        {{ repo.spec.repository }}
      </router-link>: {{ repo.status.message }}
    </Banner>

    <div
      v-if="hasSchema && !repositories.length"
      class="empty-state"
    >
      <h2>{{ t('agentic.overview.empty.title') }}</h2>
      <p class="text-muted mb-20">
        {{ t('agentic.overview.empty.description') }}
      </p>
      <router-link
        :to="createRepositoryLocation"
        class="btn role-primary"
      >
        {{ t('agentic.overview.empty.action') }}
      </router-link>
    </div>

    <template v-else-if="hasSchema">
      <AgentStats
        :daily-lists="dailyLists"
        :days="days"
        :agent-count="agents.length"
      />

      <section class="mt-40">
        <h2>{{ t('agentic.overview.byAgent', { days }) }}</h2>
        <SortableTable
          :rows="agentRows"
          :headers="agentHeaders"
          key-field="id"
          :search="false"
          :table-actions="false"
          :row-actions="false"
          default-sort-by="aiCredits"
        >
          <template #cell:agent="{ row }">
            <router-link :to="row.agent.detailLocation">
              {{ row.nameDisplay }}
            </router-link>
            <i
              v-if="row.agent.dispatchable"
              v-clean-tooltip="t('agentic.agent.dispatchable')"
              class="icon icon-play text-muted ml-5"
            />
          </template>
        </SortableTable>
      </section>

      <section
        v-if="runSchema"
        class="mt-40"
      >
        <h2>{{ t('agentic.overview.recentRuns') }}</h2>
        <ResourceTable
          :schema="runSchema"
          :rows="recentRuns"
          :table-actions="false"
          :groupable="false"
          :search="false"
          :paging="false"
        />
      </section>
    </template>
  </div>
</template>

<style lang="scss" scoped>
.agentic-overview {
  .page-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 20px;

    h1 {
      margin: 0;
    }

    p {
      margin: 4px 0 0;
    }
  }

  h2 {
    font-size: 17px;
    margin-bottom: 10px;
  }

  .empty-state {
    text-align: center;
    padding: 60px 20px;
    border: 1px dashed var(--border);
    border-radius: var(--border-radius);
  }
}
</style>
