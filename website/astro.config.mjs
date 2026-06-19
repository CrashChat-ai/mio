// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import react from '@astrojs/react';
import starlightLlmsTxt from 'starlight-llms-txt';
import remarkGfm from 'remark-gfm';

export default defineConfig({
  site: 'https://mio.crashchatapp.com',
  // GFM (tables, strikethrough) for MDX — .mdx does not get it by default.
  markdown: { remarkPlugins: [remarkGfm] },
  integrations: [
    starlight({
      title: 'MIO',
      description:
        'MIO — the messaging I/O platform that normalizes every chat surface into one canonical envelope.',
      plugins: [
        starlightLlmsTxt({
          projectName: 'MIO',
          description:
            'MIO — the messaging I/O platform that normalizes every chat surface into one canonical envelope.',
        }),
      ],
      lastUpdated: true,
      sidebar: [
        {
          label: 'Get Started',
          items: ['local-dev-mio-cliq', 'self-host-quickstart'],
        },
        {
          label: 'Architecture',
          items: ['system-architecture', 'codebase-summary', 'project-overview-pdr'],
        },
        {
          label: 'Adapters & Contracts',
          items: ['adapter-authoring-guide', 'consumer-contract', 'code-standards'],
        },
        {
          label: 'Operate',
          items: ['deployment-guide', 'mio-web-deployment'],
        },
        {
          label: 'Roadmap',
          items: ['project-roadmap'],
        },
        {
          label: 'Decisions',
          items: [{ autogenerate: { directory: 'decisions' } }],
        },
      ],
    }),
    react(),
  ],
});
