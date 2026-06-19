import { defineCollection } from 'astro:content';
import { glob } from 'astro/loaders';
import { docsSchema } from '@astrojs/starlight/schema';

// Docs markdown is read in place from ../docs (bare slugs). The landing page is src/pages/index.astro.
export const collections = {
  docs: defineCollection({
    loader: glob({ pattern: ['**/[^_]*.{md,mdx}'], base: '../docs' }),
    schema: docsSchema(),
  }),
};
