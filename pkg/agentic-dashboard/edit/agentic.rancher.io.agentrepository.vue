<script>
import CreateEditView from '@shell/mixins/create-edit-view';
import CruResource from '@shell/components/CruResource';
import NameNsDescription from '@shell/components/form/NameNsDescription';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import UnitInput from '@shell/components/form/UnitInput';
import { LabeledInput } from '@components/Form/LabeledInput';
import { Checkbox } from '@components/Form/Checkbox';
import { RadioGroup } from '@components/Form/Radio';
import { Banner } from '@components/Banner';
import { SECRET } from '@shell/config/types';

const NEW_SECRET = 'new';
const EXISTING_SECRET = 'existing';
const REPO_PATTERN = /^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/;

export default {
  name:         'CruAgentRepository',
  inheritAttrs: false,

  components: {
    CruResource, NameNsDescription, LabeledSelect, UnitInput, LabeledInput, Checkbox, RadioGroup, Banner
  },

  mixins: [CreateEditView],

  async fetch() {
    this.allSecrets = await this.$store.dispatch('management/findAll', { type: SECRET });
  },

  data() {
    this.value.applyDefaults?.();

    return {
      allSecrets: [],
      secretMode: this.value.spec?.secretRef?.name ? EXISTING_SECRET : NEW_SECRET,
      token:      '',
      autoName:   '',
    };
  },

  created() {
    this.registerBeforeHook(this.createTokenSecret, 'createTokenSecret');
  },

  computed: {
    secretModeOptions() {
      return [
        { label: this.t('agentic.repository.secret.new'), value: NEW_SECRET },
        { label: this.t('agentic.repository.secret.existing'), value: EXISTING_SECRET },
      ];
    },

    secretOptions() {
      const ns = this.value.metadata?.namespace;

      return this.allSecrets
        .filter((s) => s.metadata.namespace === ns && s._type === 'Opaque')
        .map((s) => s.metadata.name);
    },

    repositoryValid() {
      return REPO_PATTERN.test(this.value.spec?.repository || '');
    },

    validationPassed() {
      const secretOk = this.secretMode === NEW_SECRET ? !!this.token : !!this.value.spec?.secretRef?.name;

      return !!this.value.metadata?.name && this.repositoryValid && secretOk;
    },

    syncMinutes: {
      get() {
        const m = `${ this.value.spec?.syncInterval || '10m' }`.match(/^(\d+)m$/);

        return m ? Number(m[1]) : 10;
      },
      set(v) {
        this.value.spec.syncInterval = `${ Math.max(1, Number(v) || 10) }m`;
      }
    },
  },

  watch: {
    // Name the resource after the repo by default, e.g. rancher/dashboard -> dashboard
    'value.spec.repository'(neu) {
      const name = this.value.metadata?.name;

      if (this.isCreate && neu && REPO_PATTERN.test(neu) && (!name || name === this.autoName)) {
        this.autoName = neu.split('/')[1].toLowerCase().replace(/[^a-z0-9-]/g, '-');
        this.value.metadata.name = this.autoName;
      }
    },
  },

  methods: {
    // Store a pasted token in a new Secret next to the repository, then reference it
    async createTokenSecret() {
      if (this.secretMode !== NEW_SECRET || !this.token) {
        return;
      }
      const secret = await this.$store.dispatch('management/create', {
        type:     SECRET,
        _type:    'Opaque',
        metadata: {
          namespace:    this.value.metadata.namespace,
          generateName: `${ this.value.metadata.name }-github-`,
          labels:       { 'agentic.rancher.io/repository': this.value.metadata.name },
        },
        data: { token: btoa(this.token) },
      });
      const saved = await secret.save();

      this.value.spec.secretRef = { name: saved.metadata.name, key: 'token' };
      this.secretMode = EXISTING_SECRET;
      this.token = '';
    },
  }
};
</script>

<template>
  <CruResource
    :done-route="doneRoute"
    :mode="mode"
    :resource="value"
    :subtypes="[]"
    :validation-passed="validationPassed"
    :errors="errors"
    @error="e=>errors = e"
    @finish="save"
    @cancel="done"
  >
    <NameNsDescription
      :value="value"
      :mode="mode"
      :register-before-hook="registerBeforeHook"
    />

    <h3>{{ t('agentic.repository.sections.source') }}</h3>
    <div class="row mb-20">
      <div class="col span-6">
        <LabeledInput
          v-model:value="value.spec.repository"
          :mode="mode"
          :label="t('agentic.repository.fields.repository')"
          :placeholder="t('agentic.repository.fields.repositoryPlaceholder')"
          :rules="[() => !value.spec.repository || repositoryValid ? undefined : t('agentic.repository.fields.repositoryInvalid')]"
          :disabled="!isCreate"
          required
        />
      </div>
      <div class="col span-3">
        <LabeledInput
          v-model:value="value.spec.branch"
          :mode="mode"
          :label="t('agentic.repository.fields.branch')"
          :placeholder="t('agentic.repository.fields.branchPlaceholder')"
        />
      </div>
      <div class="col span-3">
        <LabeledInput
          v-model:value="value.spec.workflowsPath"
          :mode="mode"
          :label="t('agentic.repository.fields.workflowsPath')"
        />
      </div>
    </div>

    <h3>{{ t('agentic.repository.sections.auth') }}</h3>
    <Banner
      color="info"
      :label="t('agentic.repository.secret.help')"
    />
    <div class="row mb-10">
      <div class="col span-12">
        <RadioGroup
          v-model:value="secretMode"
          name="secretMode"
          :mode="mode"
          :options="secretModeOptions"
          :row="true"
        />
      </div>
    </div>
    <div class="row mb-20">
      <div
        v-if="secretMode === 'new'"
        class="col span-6"
      >
        <LabeledInput
          v-model:value="token"
          type="password"
          :mode="mode"
          :label="t('agentic.repository.secret.token')"
          required
        />
      </div>
      <div
        v-else
        class="col span-6"
      >
        <LabeledSelect
          v-model:value="value.spec.secretRef.name"
          :mode="mode"
          :options="secretOptions"
          :label="t('agentic.repository.secret.name')"
          required
        />
      </div>
      <div
        v-if="secretMode === 'existing'"
        class="col span-3"
      >
        <LabeledInput
          v-model:value="value.spec.secretRef.key"
          :mode="mode"
          :label="t('agentic.repository.secret.key')"
        />
      </div>
    </div>

    <h3>{{ t('agentic.repository.sections.sync') }}</h3>
    <div class="row mb-20">
      <div class="col span-3">
        <UnitInput
          v-model:value="syncMinutes"
          :mode="mode"
          :label="t('agentic.repository.fields.syncInterval')"
          :suffix="t('agentic.repository.fields.minutesSuffix')"
          :min="1"
        />
      </div>
      <div class="col span-3">
        <UnitInput
          v-model:value="value.spec.backfillDays"
          :mode="mode"
          :label="t('agentic.repository.fields.backfillDays')"
          :suffix="t('agentic.repository.fields.daysSuffix')"
          :min="0"
          :max="90"
        />
      </div>
      <div class="col span-3">
        <UnitInput
          v-model:value="value.spec.maxRunsPerAgent"
          :mode="mode"
          :label="t('agentic.repository.fields.maxRunsPerAgent')"
          :suffix="t('agentic.repository.fields.runsSuffix')"
          :min="1"
          :max="500"
        />
      </div>
      <div class="col span-3 suspend">
        <Checkbox
          v-model:value="value.spec.suspend"
          :mode="mode"
          :label="t('agentic.repository.fields.suspend')"
        />
      </div>
    </div>
  </CruResource>
</template>

<style lang="scss" scoped>
.suspend {
  display: flex;
  align-items: center;
}
</style>
