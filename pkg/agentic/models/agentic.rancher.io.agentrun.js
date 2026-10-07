import SteveModel from '@shell/plugins/steve/steve-class';
import { totalTokens } from '../utils/stats';

const PHASE_STATE = {
  Pending:    'pending',
  Dispatched: 'pending',
  Queued:     'pending',
  Running:    'in-progress',
  Succeeded:  'succeeded',
  Failed:     'failed',
  Cancelled:  'cancelled',
  Error:      'error',
};

export default class AgentRun extends SteveModel {
  get phase() {
    return this.status?.phase || 'Pending';
  }

  // Drives the shared state badge colours in lists and the masthead
  get state() {
    return PHASE_STATE[this.phase] || 'unknown';
  }

  get stateDisplay() {
    return this.phase;
  }

  // Only surface the message when something went wrong; "Run succeeded" under every badge is noise
  get stateDescription() {
    return this.phase === 'Error' ? this.status?.message : undefined;
  }

  get isFinished() {
    return ['Succeeded', 'Failed', 'Cancelled', 'Error'].includes(this.phase);
  }

  get agentName() {
    return this.spec?.agentRef;
  }

  get agent() {
    return this.$rootGetters['management/byId']('agentic.rancher.io.agent', `${ this.metadata.namespace }/${ this.agentName }`);
  }

  get agentDisplay() {
    return this.agent?.nameDisplay || this.agentName;
  }

  get agentLocation() {
    return this.agent?.detailLocation;
  }

  get origin() {
    return this.spec?.origin || 'rancher';
  }

  get startTime() {
    return this.status?.startTime || this.status?.dispatchTime || this.metadata?.creationTimestamp;
  }

  get durationSeconds() {
    return this.status?.durationSeconds;
  }

  get aiCredits() {
    return this.status?.usage?.aiCredits;
  }

  get tokenCount() {
    return this.status?.usage ? totalTokens(this.status.usage) : undefined;
  }

  get outputs() {
    return this.status?.outputs || [];
  }

  get outputsSummary() {
    const counts = {};

    this.outputs.forEach((o) => {
      const kind = o.kind || 'other';

      counts[kind] = (counts[kind] || 0) + 1;
    });

    return counts;
  }

  get pullRequests() {
    return this.outputs.filter((o) => o.kind === 'pullRequest');
  }

  get githubUrl() {
    return this.status?.htmlUrl;
  }

  get canUpdate() {
    return false;
  }

  get canCustomEdit() {
    return false;
  }

  get _availableActions() {
    const out = super._availableActions;

    out.unshift({
      action:  'openOnGitHub',
      label:   this.t('agentic.agent.openGitHub'),
      icon:    'icon icon-external-link',
      enabled: !!this.githubUrl,
    }, { divider: true });

    return out;
  }

  openOnGitHub() {
    window.open(this.githubUrl, '_blank', 'noopener');
  }
}
