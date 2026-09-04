import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  product: [
    {type: 'category', label: 'Introduction', items: ['index', 'concepts', 'quickstart']},
    {type: 'category', label: 'Install and setup', items: ['installation', 'setup', 'client', 'server']},
    {type: 'category', label: 'Usage', items: ['basic-usage', 'advanced-usage', 'cli-reference', 'cookbook']},
    {type: 'category', label: 'Executors and AUTO', items: ['executors', 'auto-orchestration', 'auto-scheduler', 'quota-routing', 'orchestration', 'ux-audit']},
    {type: 'category', label: 'Knowledge', items: ['memory', 'context-guide', 'multi-server', 'connections', 'mcp-web']},
    {type: 'category', label: 'Control and fidelity', items: ['skill-control-plane', 'working-context', 'compression-provider', 'caveman-canary']},
    {type: 'category', label: 'Operations', items: ['operations', 'update-rollback', 'backup-restore', 'production-compatibility', 'production-inventory', 'canary-rollout']},
    {type: 'category', label: 'Security and support', items: ['security', 'skill-control-plane-threat-model', 'troubleshooting', 'faq', 'known-limitations']},
    {type: 'category', label: 'Architecture and project', items: ['architecture', 'adrs', 'development', 'contributing', 'releases']},
  ],
};

export default sidebars;
