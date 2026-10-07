<script setup lang="ts">
withDefaults(defineProps<{
  label: string;
  value: string;
  detail?: string;
}>(), { detail: '' });
</script>

<template>
  <div class="stat-tile">
    <div class="label text-muted">
      {{ label }}
    </div>
    <div class="value">
      {{ value }}
    </div>
    <div
      v-if="detail"
      class="detail text-muted"
    >
      {{ detail }}
    </div>
  </div>
</template>

<style lang="scss" scoped>
.stat-tile {
  border: 1px solid var(--border);
  border-radius: var(--border-radius);
  padding: 12px 16px;
  min-width: 0;

  .label {
    font-size: 12px;
  }

  .value {
    font-size: 24px;
    font-weight: 600;
    line-height: 1.3;
    font-variant-numeric: tabular-nums;
  }

  .detail {
    font-size: 12px;
  }
}
</style>
