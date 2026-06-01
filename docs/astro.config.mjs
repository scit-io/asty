// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

// https://astro.build/config
export default defineConfig({
	// Public URL of the deployed site — used for canonical links,
	// Open Graph URLs, and the sitemap. Update if you point a custom
	// domain at the Firebase project.
	site: 'https://asty.web.app',
	integrations: [
		starlight({
			title: 'Asty Docs',
			sidebar: [
				{
					label: 'Guides',
					items: [
						// Each item here is one entry in the navigation menu.
						{ label: 'Example Guide', slug: 'guides/example' },
					],
				},
				{
					label: 'Reference',
					items: [{ autogenerate: { directory: 'reference' } }],
				},
			],
		}),
	],
});
