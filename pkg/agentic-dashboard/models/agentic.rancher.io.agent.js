import SteveModel from '@shell/plugins/steve/steve-class';
import {
  successRate, averageDuration, totalTokens, emptyTotals, addInto
} from '../utils/stats';

export default class Agent extends SteveModel {
  get nameDisplay() {
    return this.spec?.displayName || this.metadata?.name;
  }

  get description() {
    return this.spec?.description || '';
  }

  get totals() {
    return addInto(emptyTotals(), this.status?.totals || {});
  }

  get daily() {
    return this.status?.daily || [];
  }

  get runCount() {
    return this.totals.runs;
  }

  get successRate() {
    return successRate(this.totals);
  }

  get successRateDisplay() {
    const rate = this.successRate;

    return rate === null ? '—' : `${ rate }%`;
  }

  get averageDuration() {
    return averageDuration(this.totals);
  }

  get aiCredits() {
    return this.totals.aiCredits;
  }

  get tokenCount() {
    return totalTokens(this.totals);
  }

  get lastRunTime() {
    return this.status?.lastRun?.startTime;
  }

  get triggers() {
    return this.spec?.triggers || [];
  }

  get triggersDisplay() {
    return this.triggers.join(', ');
  }

  get dispatchable() {
    return !!this.spec?.dispatchable;
  }

  get githubUrl() {
    return this.spec?.htmlUrl;
  }

  get workflowState() {
    return this.status?.state;
  }

  // Agents are owned by the controller, so the UI never edits or creates them directly
  get canUpdate() {
    return false;
  }

  get canCustomEdit() {
    return false;
  }

  get canCreate() {
    return false;
  }

  get canRun() {
    const runSchema = this.$rootGetters['management/schemaFor']('agentic.rancher.io.agentrun');
    const canCreateRun = !!runSchema?.collectionMethods?.find((m) => m.toUpperCase() === 'POST');

    return this.dispatchable && canCreateRun && !`${ this.workflowState || '' }`.startsWith('disabled');
  }

  get _availableActions() {
    const out = super._availableActions;

    out.unshift({
      action:   'runAgent',
      label:    this.t('agentic.agent.run.action'),
      icon:     'icon icon-play',
      enabled:  this.canRun,
      bulkable: false,
    }, {
      action:  'openOnGitHub',
      label:   this.t('agentic.agent.openGitHub'),
      icon:    'icon icon-external-link',
      enabled: !!this.githubUrl,
    }, { divider: true });

    return out;
  }

  runAgent() {
    this.$dispatch('promptModal', {
      component:      'RunAgentDialog',
      componentProps: { agent: this },
      modalWidth:     '560px',
    });
  }

  openOnGitHub() {
    window.open(this.githubUrl, '_blank', 'noopener');
  }
}
