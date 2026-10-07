<script>
import { Card } from '@components/Card';
import { Banner } from '@components/Banner';
import { LabeledInput } from '@components/Form/LabeledInput';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import AsyncButton from '@shell/components/AsyncButton';
import { exceptionToErrorsArray } from '@shell/utils/error';
import { AGENTIC, API_VERSION, LABELS } from '../config/types';

export default {
  emits: ['close'],

  components: {
    Card, Banner, LabeledInput, LabeledSelect, AsyncButton
  },

  props: {
    agent: {
      type:     Object,
      required: true
    }
  },

  data() {
    const inputs = {};

    (this.agent.spec?.dispatchInputs || []).forEach((i) => {
      inputs[i.name] = i.default || '';
    });

    return {
      ref: '', inputs, errors: []
    };
  },

  computed: {
    dispatchInputs() {
      return this.agent.spec?.dispatchInputs || [];
    },

    missingRequired() {
      return this.dispatchInputs.some((i) => i.required && !this.inputs[i.name]);
    }
  },

  methods: {
    async run(buttonCb) {
      this.errors = [];

      try {
        const inputs = Object.fromEntries(Object.entries(this.inputs).filter(([, v]) => v !== ''));
        const run = await this.$store.dispatch('management/create', {
          type:       AGENTIC.RUN,
          apiVersion: API_VERSION,
          kind:       'AgentRun',
          metadata:   {
            namespace:    this.agent.metadata.namespace,
            generateName: `${ this.agent.metadata.name.slice(0, 50) }-`,
            labels:       {
              [LABELS.AGENT]:  this.agent.metadata.name,
              [LABELS.ORIGIN]: 'rancher',
            }
          },
          spec: {
            agentRef: this.agent.metadata.name,
            origin:   'rancher',
            ...(this.ref ? { ref: this.ref } : {}),
            ...(Object.keys(inputs).length ? { inputs } : {}),
          }
        });

        await run.save();
        buttonCb(true);
        this.$emit('close');
        this.$router.push(this.agent.detailLocation);
      } catch (err) {
        this.errors = exceptionToErrorsArray(err);
        buttonCb(false);
      }
    }
  }
};
</script>

<template>
  <Card
    class="run-agent"
    :show-highlight-border="false"
  >
    <template #title>
      <h4 class="text-default-text">
        {{ t('agentic.agent.run.title', { name: agent.nameDisplay }) }}
      </h4>
    </template>

    <template #body>
      <p class="mb-15">
        {{ t('agentic.agent.run.description', { repo: agent.spec.repository }) }}
      </p>
      <Banner
        color="warning"
        :label="t('agentic.agent.run.costWarning')"
      />
      <LabeledInput
        v-model:value="ref"
        class="mb-15"
        :label="t('agentic.agent.run.ref')"
        :placeholder="t('agentic.agent.run.refPlaceholder')"
      />
      <template
        v-for="input in dispatchInputs"
        :key="input.name"
      >
        <LabeledSelect
          v-if="input.options && input.options.length"
          v-model:value="inputs[input.name]"
          class="mb-15"
          :label="input.name"
          :tooltip="input.description"
          :options="input.options"
          :required="input.required"
        />
        <LabeledInput
          v-else
          v-model:value="inputs[input.name]"
          class="mb-15"
          :label="input.name"
          :tooltip="input.description"
          :required="input.required"
        />
      </template>
      <Banner
        v-for="(err, i) in errors"
        :key="i"
        color="error"
        :label="err"
      />
    </template>

    <template #actions>
      <div class="actions">
        <button
          class="btn role-secondary mr-10"
          @click="$emit('close')"
        >
          {{ t('generic.cancel') }}
        </button>
        <AsyncButton
          :action-label="t('agentic.agent.run.confirm')"
          :disabled="missingRequired"
          @click="run"
        />
      </div>
    </template>
  </Card>
</template>

<style lang="scss" scoped>
.run-agent {
  margin: 0;

  .actions {
    display: flex;
    justify-content: flex-end;
    width: 100%;
  }
}
</style>
