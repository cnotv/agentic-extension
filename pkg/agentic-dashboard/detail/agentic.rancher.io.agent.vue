<script>
import ResourceTabs from '@shell/components/form/ResourceTabs';
import Tab from '@shell/components/Tabbed/Tab';
import ResourceTable from '@shell/components/ResourceTable';
import ButtonGroup from '@shell/components/ButtonGroup';
import { Banner } from '@components/Banner';
import AgentStats from '../components/AgentStats.vue';
import { AGENTIC } from '../config/types';

export default {
  name: 'AgentDetail',

  components: {
    ResourceTabs, Tab, ResourceTable, ButtonGroup, Banner, AgentStats
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
    await this.$store.dispatch('management/findAll', { type: AGENTIC.RUN });
  },

  data() {
    return {
      days:       30,
      dayOptions: [7, 30, 90].map((d) => ({ label: this.t('agentic.overview.days', { d }), value: d })),
    };
  },

  computed: {
    runSchema() {
      return this.$store.getters['management/schemaFor'](AGENTIC.RUN);
    },

    runs() {
      return this.$store.getters['management/all'](AGENTIC.RUN)
        .filter((r) => r.metadata.namespace === this.value.metadata.namespace && r.spec?.agentRef === this.value.metadata.name);
    },

    inputs() {
      return this.value.spec?.dispatchInputs || [];
    },

    details() {
      const s = this.value.spec || {};

      return [
        { label: this.t('agentic.agent.fields.repository'), value: s.repository },
        {
          label: this.t('agentic.agent.fields.workflow'), value: s.workflowFile, href: s.htmlUrl
        },
        { label: this.t('agentic.agent.fields.engine'), value: [s.engine, s.model].filter(Boolean).join(' · ') },
        { label: this.t('agentic.agent.fields.schedule'), value: s.schedule },
        { label: this.t('agentic.agent.fields.slashCommand'), value: s.slashCommand ? `/${ s.slashCommand }` : '' },
        { label: this.t('agentic.agent.fields.timeout'), value: s.timeoutMinutes ? `${ s.timeoutMinutes }m` : '' },
        { label: this.t('agentic.agent.fields.state'), value: this.value.status?.state },
      ].filter((d) => d.value);
    }
  },
};
</script>

<template>
  <div class="agent-detail">
    <div class="summary">
      <div class="triggers">
        <span class="text-muted mr-10">{{ t('agentic.headers.triggers') }}</span>
        <span
          v-for="trigger in value.triggers"
          :key="trigger"
          class="trigger"
        >{{ trigger }}</span>
      </div>
      <div class="actions">
        <button
          v-if="value.dispatchable"
          class="btn role-primary"
          :disabled="!value.canRun"
          @click="value.runAgent()"
        >
          <i class="icon icon-play mr-5" />{{ t('agentic.agent.run.action') }}
        </button>
        <Banner
          v-else
          color="info"
          class="m-0"
          :label="t('agentic.agent.notDispatchable')"
        />
        <ButtonGroup
          v-model:value="days"
          :options="dayOptions"
        />
      </div>
    </div>

    <AgentStats
      :daily-lists="[value.daily]"
      :days="days"
    />

    <ResourceTabs
      class="mt-40"
      :value="value"
      :mode="mode"
      :need-related="false"
    >
      <Tab
        name="runs"
        :label="t('agentic.agent.tabs.runs')"
        :weight="10"
      >
        <ResourceTable
          v-if="runSchema"
          :schema="runSchema"
          :rows="runs"
          :table-actions="false"
          :groupable="false"
          :loading="$fetchState.pending"
        />
      </Tab>
      <Tab
        name="configuration"
        :label="t('agentic.agent.tabs.configuration')"
        :weight="9"
      >
        <dl class="details">
          <template
            v-for="d in details"
            :key="d.label"
          >
            <dt>{{ d.label }}</dt>
            <dd>
              <a
                v-if="d.href"
                :href="d.href"
                target="_blank"
                rel="noopener noreferrer nofollow"
              >{{ d.value }} <i class="icon icon-external-link" /></a>
              <template v-else>
                {{ d.value }}
              </template>
            </dd>
          </template>
          <dt>{{ t('agentic.agent.fields.safeOutputs') }}</dt>
          <dd>{{ (value.spec.safeOutputs || []).join(', ') || '—' }}</dd>
        </dl>

        <template v-if="inputs.length">
          <h3 class="mt-20">
            {{ t('agentic.agent.fields.inputs') }}
          </h3>
          <dl class="details">
            <template
              v-for="input in inputs"
              :key="input.name"
            >
              <dt>{{ input.name }}<span v-if="input.required"> *</span></dt>
              <dd>{{ input.description || '—' }}</dd>
            </template>
          </dl>
        </template>
      </Tab>
    </ResourceTabs>
  </div>
</template>

<style lang="scss" scoped>
.agent-detail {
  .summary {
    margin-bottom: 20px;

    .triggers {
      margin-bottom: 15px;
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 6px;
    }

    .trigger {
      border: 1px solid var(--border);
      border-radius: var(--border-radius);
      padding: 1px 8px;
      font-size: 12px;
      font-family: monospace;
    }

    .actions {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 20px;
    }
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
      word-break: break-word;
    }
  }
}
</style>
