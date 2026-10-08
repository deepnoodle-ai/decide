// Adds each page's link-preview image, made by src/pages/og/[...route].ts.
import { defineRouteMiddleware } from '@astrojs/starlight/route-data';

export const onRequest = defineRouteMiddleware((context) => {
	const id = context.locals.starlightRoute.entry.id;
	const image = new URL(`/og/${id === '' ? 'index' : id}.png`, context.site);
	const { head } = context.locals.starlightRoute;
	head.push({ tag: 'meta', attrs: { property: 'og:image', content: image.href } });
	head.push({ tag: 'meta', attrs: { property: 'og:image:width', content: '1200' } });
	head.push({ tag: 'meta', attrs: { property: 'og:image:height', content: '630' } });
	head.push({ tag: 'meta', attrs: { name: 'twitter:image', content: image.href } });
});
