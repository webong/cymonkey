import { defineConfig } from 'wxt';

export default defineConfig({
  outDirTemplate: '{{browser}}-mv{{manifestVersion}}',
  manifest: ({ browser }) => ({
    name: 'Cymonkey Browser Extension',
    description: 'Cymonkey browser extension using Jangolova interaction libraries and runtime-activated Xallet Spook integration',
    permissions: [
      'declarativeNetRequest',
      'declarativeNetRequestWithHostAccess',
      'scripting',
      'storage',
      'tabs',
      ...(browser === 'chrome' || browser === 'edge' ? ['offscreen'] : []),
      ...(browser === 'safari' ? [] : ['management', 'userScripts']),
    ],
    host_permissions: ['<all_urls>'],
    sandbox: browser === 'chrome' || browser === 'edge' ? {pages: ['runtime-sandbox.html']} : undefined,
    externally_connectable: browser === 'safari' ? undefined : { ids: ['*'] },
    web_accessible_resources: [
      {
        resources: ['runtime-sandbox.html', 'augmentations/*'],
        matches: ['<all_urls>'],
      },
    ],
    browser_specific_settings: {
      gecko: {
        id: 'browser-cymonkey@cymonkey.dev',
        strict_min_version: '128.0',
        data_collection_permissions: {
          required: ['none'],
        },
      },
    },
  }),
});
