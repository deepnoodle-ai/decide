// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';
import starlightLinksValidator from 'starlight-links-validator';

const repo = 'https://github.com/deepnoodle-ai/decide';

// Cloudflare Web Analytics, which sets no cookies. site.yml sets the token
// from the repository variable CF_BEACON_TOKEN on release builds only, so
// previews and local builds count nothing.
const beacon = process.env.CF_BEACON_TOKEN;

export default defineConfig({
	site: 'https://decide.deepnoodle.ai',
	integrations: [
		starlight({
			title: 'decide',
			description:
				'A Go library and CLI for decision models: ask typed questions, get answers with probabilities.',
			favicon: '/favicon.svg',
			// Fails on broken paths and headings, including absolute same-site links.
			plugins: [starlightLinksValidator({ sameSitePolicy: 'validate' })],
			routeMiddleware: './src/routeData.ts',
			head: beacon
				? [
						{
							tag: 'script',
							attrs: {
								defer: true,
								src: 'https://static.cloudflareinsights.com/beacon.min.js',
								'data-cf-beacon': JSON.stringify({ token: beacon }),
							},
						},
					]
				: [],
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
				{
					label: 'Tutorials',
					items: [
						{ label: 'Check agent commands', link: '/tutorials/check-agent-commands/' },
						{ label: 'Triage new issues', link: '/tutorials/triage-issues/' },
						{ label: 'Review pull requests', link: '/tutorials/review-pull-requests/' },
						{ label: 'Find security bugs', link: '/tutorials/find-security-bugs/' },
						{ label: 'Decide from Go', link: '/tutorials/decide-from-go/' },
						{ label: 'Write a template', link: '/tutorials/write-a-template/' },
					],
				},
				{
					label: 'Reference',
					items: [
						{ label: 'Overview', link: '/reference/' },
						{ label: 'Commands and flags', link: '/reference/cli/' },
						{ label: 'Items', link: '/reference/items/' },
						{ label: 'Diffs', link: '/reference/diffs/' },
						{ label: 'Templates', link: '/reference/templates/' },
						{ label: 'Providers', link: '/reference/providers/' },
						{ label: 'Output formats', link: '/reference/output/' },
						{ label: 'The answer cache', link: '/reference/cache/' },
						{ label: 'Claude Code', link: '/reference/claude-code/' },
						{ label: 'For agents', link: '/reference/agents/' },
					],
				},
				{
					label: 'Recipes',
					items: [
						{ label: 'All recipes', link: '/recipes/' },
						{ label: 'GitHub Actions notes', link: '/recipes/#run-decide-in-github-actions' },
						{ label: 'Comment on a pull request', link: '/recipes/#comment-on-the-pull-request' },
						{ label: 'Route issues to a queue', link: '/recipes/#route-new-issues-to-a-queue' },
						{ label: 'Other CI systems', link: '/recipes/#other-ci-systems' },
						{ label: 'Pre-commit hook', link: '/recipes/#check-changes-before-you-commit' },
						{ label: 'Export to a spreadsheet', link: '/recipes/#export-results-to-a-spreadsheet' },
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
