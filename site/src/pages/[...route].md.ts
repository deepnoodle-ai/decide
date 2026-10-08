import type { APIRoute } from 'astro';
import { markdown, markdownPages, markdownPath } from '../lib/markdown';

export async function getStaticPaths() {
	return (await markdownPages()).map((entry) => ({
		params: { route: markdownPath(entry).slice(1, -3) },
		props: { entry },
	}));
}

export const GET: APIRoute = ({ props, site }) =>
	new Response(markdown(props.entry, site!), { headers: { 'Content-Type': 'text/markdown; charset=utf-8' } });
