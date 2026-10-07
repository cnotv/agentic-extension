<script>
import PaginatedResourceTable from '@shell/components/PaginatedResourceTable';
import { AGENTIC } from '../config/types';

export default {
  name: 'AgentRunList',

  components: { PaginatedResourceTable },

  props: {
    resource: {
      type:     String,
      required: true,
    },
    schema: {
      type:     Object,
      required: true,
    },
    useQueryParamsForSimpleFiltering: {
      type:    Boolean,
      default: false
    }
  },

  methods: {
    // Agents are needed to show each run's agent display name
    async fetchSecondaryResources() {
      await this.$store.dispatch('management/findAll', { type: AGENTIC.AGENT });
    }
  }
};
</script>

<template>
  <PaginatedResourceTable
    :schema="schema"
    :use-query-params-for-simple-filtering="useQueryParamsForSimpleFiltering"
    :fetch-secondary-resources="fetchSecondaryResources"
  />
</template>
