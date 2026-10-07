import { STATE, NAME, NAMESPACE, AGE } from '@shell/config/table-headers';
import { formatCredits, formatDuration } from '../utils/stats';

// The model getters read by the columns below
interface Row {
  spec?: { repository?: string };
  status?: { lastSyncTime?: string; event?: string };
  agentCount?: number;
  rateLimitDisplay?: string;
  triggersDisplay?: string;
  dispatchable?: boolean;
  runCount?: number;
  successRateDisplay?: string;
  aiCredits?: number;
  lastRunTime?: string;
  agentDisplay?: string;
  origin?: string;
  durationSeconds?: number;
  startTime?: string;
}

export const REPOSITORY_HEADERS = [
  STATE,
  NAME,
  NAMESPACE,
  {
    name:     'repository',
    labelKey: 'agentic.headers.repository',
    value:    'spec.repository',
    getValue: (row: Row) => row.spec?.repository,
    sort:     ['spec.repository'],
  },
  {
    name:     'agents',
    labelKey: 'agentic.headers.agents',
    value:    'agentCount',
    getValue: (row: Row) => row.agentCount,
    sort:     ['agentCount'],
    align:    'right',
    width:    90,
  },
  {
    name:     'rateLimit',
    labelKey: 'agentic.headers.rateLimit',
    value:    'rateLimitDisplay',
    getValue: (row: Row) => row.rateLimitDisplay,
    align:    'right',
  },
  {
    name:      'lastSync',
    labelKey:  'agentic.headers.lastSync',
    value:     'status.lastSyncTime',
    getValue:  (row: Row) => row.status?.lastSyncTime,
    sort:      ['status.lastSyncTime'],
    formatter: 'LiveDate',
  },
  AGE,
];

export const AGENT_HEADERS = [
  NAME,
  NAMESPACE,
  {
    name:     'repository',
    labelKey: 'agentic.headers.repository',
    value:    'spec.repository',
    getValue: (row: Row) => row.spec?.repository,
    sort:     ['spec.repository'],
  },
  {
    name:     'triggers',
    labelKey: 'agentic.headers.triggers',
    value:    'triggersDisplay',
    getValue: (row: Row) => row.triggersDisplay,
    sort:     ['triggersDisplay'],
  },
  {
    name:      'dispatchable',
    labelKey:  'agentic.headers.dispatchable',
    value:     'dispatchable',
    getValue:  (row: Row) => row.dispatchable,
    sort:      ['dispatchable'],
    formatter: 'Checked',
    align:     'center',
    width:     110,
  },
  {
    name:     'runs',
    labelKey: 'agentic.headers.runs',
    value:    'runCount',
    getValue: (row: Row) => row.runCount,
    sort:     ['runCount'],
    align:    'right',
    width:    80,
  },
  {
    name:     'successRate',
    labelKey: 'agentic.headers.successRate',
    value:    'successRateDisplay',
    getValue: (row: Row) => row.successRateDisplay,
    sort:     ['successRate'],
    align:    'right',
    width:    100,
  },
  {
    name:     'aiCredits',
    labelKey: 'agentic.headers.aiCredits',
    value:    'aiCredits',
    getValue: (row: Row) => formatCredits(row.aiCredits),
    sort:     ['aiCredits'],
    align:    'right',
    width:    100,
  },
  {
    name:      'lastRun',
    labelKey:  'agentic.headers.lastRun',
    value:     'lastRunTime',
    getValue:  (row: Row) => row.lastRunTime,
    sort:      ['lastRunTime'],
    formatter: 'LiveDate',
  },
];

export const RUN_HEADERS = [
  STATE,
  NAME,
  {
    name:     'agent',
    labelKey: 'agentic.headers.agent',
    value:    'agentDisplay',
    getValue: (row: Row) => row.agentDisplay,
    sort:     ['agentDisplay'],
  },
  {
    name:     'origin',
    labelKey: 'agentic.headers.origin',
    value:    'origin',
    getValue: (row: Row) => row.origin,
    sort:     ['origin'],
    width:    90,
  },
  {
    name:     'event',
    labelKey: 'agentic.headers.event',
    value:    'status.event',
    getValue: (row: Row) => row.status?.event,
    sort:     ['status.event'],
  },
  {
    name:     'aiCredits',
    labelKey: 'agentic.headers.aiCredits',
    value:    'aiCredits',
    getValue: (row: Row) => formatCredits(row.aiCredits),
    sort:     ['aiCredits'],
    align:    'right',
    width:    100,
  },
  {
    name:     'duration',
    labelKey: 'agentic.headers.duration',
    value:    'durationSeconds',
    getValue: (row: Row) => formatDuration(row.durationSeconds),
    sort:     ['durationSeconds'],
    align:    'right',
    width:    100,
  },
  {
    name:      'started',
    labelKey:  'agentic.headers.started',
    value:     'startTime',
    getValue:  (row: Row) => row.startTime,
    sort:      ['startTime:desc'],
    formatter: 'LiveDate',
  },
];
