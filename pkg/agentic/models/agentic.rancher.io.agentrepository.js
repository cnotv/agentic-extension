import SteveModel from '@shell/plugins/steve/steve-class';

const PHASE_STATE = {
  Pending: 'pending',
  Syncing: 'in-progress',
  Ready:   'active',
  Error:   'error',
};

export default class AgentRepository extends SteveModel {
  get state() {
    if (this.spec?.suspend) {
      return 'paused';
    }

    return PHASE_STATE[this.status?.phase] || 'pending';
  }

  get stateDisplay() {
    return this.spec?.suspend ? 'Suspended' : (this.status?.phase || 'Pending');
  }

  get stateDescription() {
    return this.status?.phase === 'Error' ? this.status?.message : undefined;
  }

  get githubUrl() {
    return this.status?.htmlUrl || (this.spec?.repository ? `https://github.com/${ this.spec.repository }` : undefined);
  }

  get agentCount() {
    return this.status?.agentCount || 0;
  }

  get rateLimitDisplay() {
    const rl = this.status?.rateLimit;

    return rl ? `${ rl.remaining } / ${ rl.limit }` : '—';
  }

  applyDefaults() {
    if (!this.spec) {
      this.spec = {};
    }
    this.spec = {
      repository:      '',
      workflowsPath:   '.github/workflows',
      secretRef:       { name: '', key: 'token' },
      syncInterval:    '10m',
      backfillDays:    14,
      maxRunsPerAgent: 50,
      suspend:         false,
      ...this.spec,
    };
  }

  get _availableActions() {
    const out = super._availableActions;

    out.unshift({
      action:  'toggleSuspend',
      label:   this.spec?.suspend ? this.t('agentic.repository.resume') : this.t('agentic.repository.suspend'),
      icon:    this.spec?.suspend ? 'icon icon-play' : 'icon icon-pause',
      enabled: this.canUpdate,
    }, { divider: true });

    return out;
  }

  async toggleSuspend() {
    this.spec.suspend = !this.spec.suspend;
    await this.save();
  }
}
