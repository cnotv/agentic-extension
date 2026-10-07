<script>
import ResourceTabs from '@shell/components/form/ResourceTabs';
import Tab from '@shell/components/Tabbed/Tab';
import SortableTable from '@shell/components/SortableTable';
import { Banner } from '@components/Banner';
import StatTile from '../components/StatTile.vue';
import { AGENTIC } from '../config/types';
import { formatCredits, formatCompact, formatDuration } from '../utils/stats';

export default {
  name: 'AgentRunDetail',

  components: {
    ResourceTabs, Tab, SortableTable, Banner, StatTile
  },

  props: {
    value: {
      type:     Object,
      required: true
    },
    mode: {
      type:    String,
      default: 'view'
    }
  },

  async fetch() {
    // Needed to resolve the agent display name and link
    await this.$store.dispatch('management/findAll', { type: AGENTIC.AGENT });
  },

  computed: {
    usage() {
      return this.value.status?.usage || {};
    },

    usageState() {
      return this.value.status?.usageState;
    },

    details() {
      const s = this.value.status || {};

      return [
        { label: this.t('agentic.run.fields.event'), value: s.event },
        { label: this.t('agentic.run.fields.actor'), value: s.actor },
        { label: this.t('agentic.run.fields.branch'), value: s.headBranch || this.value.spec?.ref },
        { label: this.t('agentic.run.fields.runNumber'), value: s.runNumber ? `#${ s.runNumber }${ s.runAttempt > 1 ? ` (attempt ${ s.runAttempt })` : '' }` : '' },
        { label: this.t('agentic.run.fields.model'), value: s.model },
        { label: this.t('agentic.run.fields.conclusion'), value: s.conclusion },
      ].filter((d) => d.value);
    },

    inputs() {
      return Object.entries(this.value.spec?.inputs || {});
    },

    outputHeaders() {
      return [
        {
          name: 'kind', labelKey: 'agentic.run.outputs.kind', value: 'kind', sort: ['kind']
        },
        {
          name: 'target', labelKey: 'agentic.run.outputs.target', value: 'target', sort: ['target']
        },
        {
          name: 'state', labelKey: 'agentic.run.outputs.state', value: 'state', sort: ['state']
        },
      ];
    },

    outputRows() {
      return this.value.outputs.map((o, i) => ({
        id:     `${ i }`,
        kind:   this.t(`agentic.run.outputs.kinds.${ o.kind || 'other' }`, {}, true) || o.type,
        type:   o.type,
        target: o.title || (o.number ? `${ o.repo || '' }#${ o.number }` : o.repo || ''),
        url:    o.url || (o.number && o.repo ? `https://github.com/${ o.repo }/issues/${ o.number }` : ''),
        state:  o.state || '',
      }));
    },
  },

  methods: {
    formatCredits, formatCompact, formatDuration
  }
};
</script>

<template>
  <div class="run-detail">
    <div class="summary">
      <span v-if="value.agentLocation">
        {{ t('agentic.run.ofAgent') }}
        <router-link :to="value.agentLocation">{{ value.agentDisplay }}</router-link>
      </span>
      <a
        v-if="value.githubUrl"
        :href="value.githubUrl"
        target="_blank"
        rel="noopener noreferrer nofollow"
        class="btn role-secondary"
      >
        {{ t('agentic.agent.openGitHub') }} <i class="icon icon-external-link ml-5" />
      </a>
    </div>

    <Banner
      v-if="value.status && value.status.message && !value.isFinished"
      color="info"
      :label="value.status.message"
    />
    <Banner
      v-else-if="value.phase === 'Error'"
      color="error"
      :label="value.status.message"
    />

    <div class="tiles">
      <StatTile
        :label="t('agentic.stats.duration')"
        :value="formatDuration(value.durationSeconds)"
      />
      <StatTile
        :label="t('agentic.stats.aiCredits')"
        :value="formatCredits(usage.aiCredits)"
        :detail="usageState === 'Unavailable' ? t('agentic.run.usageUnavailable') : ''"
      />
      <StatTile
        :label="t('agentic.stats.inputOutput')"
        :value="`${ formatCompact(usage.inputTokens) } / ${ formatCompact(usage.outputTokens) }`"
      />
      <StatTile
        :label="t('agentic.stats.cache')"
        :value="`${ formatCompact(usage.cacheReadTokens) } / ${ formatCompact(usage.cacheWriteTokens) }`"
        :detail="t('agentic.stats.cacheDetail')"
      />
    </div>

    <ResourceTabs
      class="mt-30"
      :value="value"
      :mode="mode"
      :need-related="false"
    >
      <Tab
        name="outputs"
        :label="t('agentic.run.tabs.outputs')"
        :weight="10"
      >
        <SortableTable
          :rows="outputRows"
          :headers="outputHeaders"
          key-field="id"
          :search="false"
          :table-actions="false"
          :row-actions="false"
          :no-rows-key="usageState === 'Pending' ? 'agentic.run.outputsPending' : 'agentic.run.noOutputs'"
        >
          <template #cell:target="{ row }">
            <a
              v-if="row.url"
              :href="row.url"
              target="_blank"
              rel="noopener noreferrer nofollow"
            >{{ row.target || row.url }}</a>
            <span v-else>{{ row.target }}</span>
          </template>
        </SortableTable>
      </Tab>
      <Tab
        name="details"
        :label="t('agentic.run.tabs.details')"
        :weight="9"
      >
        <dl class="details">
          <template
            v-for="d in details"
            :key="d.label"
          >
            <dt>{{ d.label }}</dt>
            <dd>{{ d.value }}</dd>
          </template>
          <template
            v-for="[k, v] in inputs"
            :key="k"
          >
            <dt>{{ t('agentic.run.fields.input', { name: k }) }}</dt>
            <dd>{{ v }}</dd>
          </template>
        </dl>
      </Tab>
    </ResourceTabs>
  </div>
</template>

<style lang="scss" scoped>
.run-detail {
  .summary {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;
  }

  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
    gap: 12px;
  }

  .details {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 8px 24px;
    margin: 0;

    dt {
      color: var(--muted);
    }

    dd {
      margin: 0;
    }
  }
}
</style>
