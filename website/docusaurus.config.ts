import type {Config} from '@docusaurus/types';
import type {Options, ThemeConfig} from '@docusaurus/preset-classic';

const config: Config = {
  title: 'IVOAI',
  tagline: 'Host-first AI client, server, memory, context and orchestration',
  favicon: 'img/favicon.svg',
  url: 'https://docs.example.invalid',
  baseUrl: '/',
  organizationName: 'ivo-lopes',
  projectName: 'ivoai',
  trailingSlash: false,
  onBrokenLinks: 'throw',
  markdown: {mermaid: true},
  themes: ['@docusaurus/theme-mermaid'],
  i18n: {defaultLocale: 'en', locales: ['en']},
  presets: [
    [
      'classic',
      {
        docs: {
          path: '../docs',
          routeBasePath: 'docs',
          sidebarPath: './sidebars.ts',
          showLastUpdateTime: false,
          showLastUpdateAuthor: false,
          editUrl: 'https://github.com/ivo-lopes/ivoai/edit/main/',
        },
        blog: false,
        theme: {customCss: './src/css/custom.css'},
        sitemap: {changefreq: 'weekly', priority: 0.5},
      } satisfies Options,
    ],
  ],
  plugins: [
    [
      '@easyops-cn/docusaurus-search-local',
      {
        hashed: true,
        indexDocs: true,
        indexBlog: false,
        indexPages: true,
        docsDir: '../docs',
        docsRouteBasePath: '/docs',
        language: ['en', 'pt'],
      },
    ],
  ],
  themeConfig: {
    navbar: {
      title: 'IVOAI',
      items: [
        {to: '/docs/quickstart', label: 'Quickstart', position: 'left'},
        {to: '/docs/cli-reference', label: 'CLI', position: 'left'},
        {to: '/docs/server', label: 'Server', position: 'left'},
        {to: '/docs/mcp-web', label: 'MCP Web', position: 'left'},
        {type: 'docsVersionDropdown', position: 'right'},
        {href: 'https://github.com/ivo-lopes/ivoai', label: 'GitHub', position: 'right'},
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {title: 'Operate', items: [{label: 'Install', to: '/docs/installation'}, {label: 'Troubleshooting', to: '/docs/troubleshooting'}]},
        {title: 'Build', items: [{label: 'Architecture', to: '/docs/architecture'}, {label: 'Development', to: '/docs/development'}]},
      ],
      copyright: `Copyright © ${new Date().getFullYear()} IVOAI. MIT licensed.`,
    },
    prism: {additionalLanguages: ['bash', 'toml', 'json']},
  } satisfies ThemeConfig,
};

export default config;
