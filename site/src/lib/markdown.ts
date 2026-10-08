// Markdown copies are built from the authored .md references, never from
// MDX component source. The human-readable pages remain the source of truth.
import { getCollection, type CollectionEntry } from 'astro:content';
import { readFileSync } from 'node:fs';

export const pagePath = (entry: CollectionEntry<'docs'>) =>
	`/${entry.id.replace(/(^|\/)index$/, '').replace(/\/$/, '')}/`.replace('//', '/');

export const markdownPath = (entry: CollectionEntry<'docs'>) => `${pagePath(entry)}index.md`;

export async function markdownPages() {
	return (await getCollection('docs', (entry) => entry.filePath?.endsWith('.md') ?? false))
		.sort((a, b) => a.id.localeCompare(b.id));
}

export function markdown(entry: CollectionEntry<'docs'>, site: URL) {
	const source = readFileSync(entry.filePath!, 'utf8').replace(/^---\r?\n[\s\S]*?\r?\n---\r?\n/, '');
	// Reference links are relative on the HTML site. Make them useful when
	// this file is fetched on its own, or copied into an agent's context.
	const body = source.replace(/\]\((\/[^)]+)\)/g, (_match, path) => `](${new URL(path, site).href})`);
	return `# ${entry.data.title}\n\n${entry.data.description}\n\nSource: ${new URL(pagePath(entry), site).href}\n\n${body}`;
}
