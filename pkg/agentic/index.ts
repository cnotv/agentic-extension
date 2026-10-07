import { importTypes } from '@rancher/auto-import';
import { IPlugin } from '@shell/core/types';
import { ProductChildResourcePage } from '@shell/core/plugin-products-external';
import { AGENTIC, PRODUCT_NAME, OVERVIEW_PAGE } from './config/types';
import { REPOSITORY_HEADERS, AGENT_HEADERS, RUN_HEADERS } from './config/headers';

// Init the package
export default function(plugin: IPlugin): void {
  // Auto-import model, detail, edit, dialog from the folders
  importTypes(plugin);

  // Provide plugin metadata from package.json
  plugin.metadata = require('./package.json');

  // `localHeaders` is honoured by the product registration but not yet in the public type
  const list = (headers: unknown[]) => ({ localHeaders: headers } as unknown as ProductChildResourcePage['listConfig']);

  plugin.addProduct({
    name:      PRODUCT_NAME,
    labelKey:  'agentic.product',
    sideBar:   { icon: { name: 'ai' }, weight: 90 },
    appHeader: { showNamespaceFilter: true },
  }, [
    {
      name:      OVERVIEW_PAGE,
      labelKey:  'agentic.overview.menu',
      component: () => import('./pages/Overview.vue'),
    },
    {
      type:       AGENTIC.AGENT,
      can:        { create: false, edit: false },
      display:    { showState: false },
      listConfig: list(AGENT_HEADERS),
    },
    {
      type:       AGENTIC.RUN,
      can:        { edit: false },
      listConfig: list(RUN_HEADERS),
    },
    {
      type:       AGENTIC.REPOSITORY,
      listConfig: list(REPOSITORY_HEADERS),
    },
  ]);
}
