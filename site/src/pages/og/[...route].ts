// A link-preview image for each page, made at build time from its title and
// description. src/routeData.ts points each page's og:image here.
import { getCollection } from 'astro:content';
import { OGImageRoute } from 'astro-og-canvas';

const entries = await getCollection('docs');
const pages = Object.fromEntries(entries.map(({ id, data }) => [id === 'index' ? 'index' : id, data]));

const font = (file: string) => `./node_modules/${file}`;

export const { getStaticPaths, GET } = await OGImageRoute({
	pages,
	getImageOptions: (_path, page: (typeof pages)[string]) => ({
		title: page.title,
		// The page's answer line, as on the page, or else its description. No
		// logo, so nothing binary is committed: the site's name ends the text.
		description: page.answer ?? `${page.description}\n\ndecide.deepnoodle.ai`,
		bgGradient: [[18, 19, 22]],
		border: { color: [46, 48, 54], width: 2, side: 'block-end' },
		padding: 72,
		fonts: [
			font('@fontsource/jetbrains-mono/files/jetbrains-mono-latin-700-normal.woff'),
			font('@fontsource/jetbrains-mono/files/jetbrains-mono-latin-400-normal.woff'),
		],
		font: {
			title: { families: ['JetBrains Mono'], weight: 'Bold', size: 64, lineHeight: 1.15, color: [243, 244, 245] },
			description: page.answer
				? { families: ['JetBrains Mono'], weight: 'Bold', size: 44, color: page.answer.startsWith('!') ? [247, 102, 111] : [76, 203, 226] }
				: { families: ['JetBrains Mono'], weight: 'Normal', size: 30, lineHeight: 1.4, color: [164, 168, 175] },
		},
	}),
});
