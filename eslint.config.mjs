import shellConfig from '@rancher/shell/eslint.config.base.mjs';

export default [
  { ignores: ['dist/**', 'dist-pkg/**', 'node_modules/**', 'controller/**', 'charts/**', 'tmp/**'] },
  ...shellConfig,
];
