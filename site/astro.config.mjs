// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

const repo = 'https://github.com/deepnoodle-ai/decide';

export default defineConfig({
	site: 'https://decide.deepnoodle.ai',
	integrations: [
		starlight({
			title: 'decide',
			description:
				'A Go library and CLI for decision models: ask typed questions, get answers with probabilities.',
			favicon: '/favicon.svg',
			social: [{ icon: 'github', label: 'GitHub', href: repo }],
			editLink: { baseUrl: `${repo}/edit/main/site/` },
			customCss: [
				'@fontsource-variable/inter/opsz.css',
				'@fontsource/jetbrains-mono/400.css',
				'@fontsource/jetbrains-mono/700.css',
				'./src/styles/theme.css',
			],
			components: {
				Hero: './src/components/Hero.astro',
				SiteTitle: './src/components/SiteTitle.astro',
				Footer: './src/components/Footer.astro',
			},
			sidebar: [
				{
					label: 'Start',
					items: [
						{ label: 'Quickstart', link: '/start/' },
						{ label: 'From Go', link: '/start/go/' },
						{ label: 'Questions and answers', link: '/start/answers/' },
					],
				},
			],
		}),
	],
	vite: {
		// /start/go/ shows a program from examples/, outside the site's root.
		server: { fs: { allow: ['..'] } },
	},
});
