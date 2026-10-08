// Reads a VHS tape from site/tapes and finds its recording. See
// "Recordings" in docs/design/docs-site.md. scripts/record.sh computes the
// same hash, and internal/cli/tapes_test.go parses tapes the same way.
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import plugin from '../../../plugin/.claude-plugin/plugin.json';

// To try recordings before they are uploaded, run record.sh --local, serve
// site/tapes/out, and set DECIDE_MEDIA to its URL.
export const mediaBase = process.env.DECIDE_MEDIA ?? 'https://files.deepnoodle.ai/decide/site';

// Astro builds from site/, and bundling moves this file, so paths start there.
const tapesDir = join(process.cwd(), 'tapes');
const read = (name: string) => readFileSync(join(tapesDir, name), 'utf8');

export interface Tape {
	/** The commands the reader sees typed, in order. */
	commands: string[];
	/** Every command, hidden ones too, in order. */
	all: string[];
	/** The recording's size in pixels, at 2x. */
	width: number;
	height: number;
	/** The terminal's width in cells. */
	cols: number;
	mp4: string;
	png: string;
	txt: string;
}

const typeOrEnter = /Type(?:@\S+)?\s+(?:"([^"]*)"|'([^']*)'|`([^`]*)`)|\bEnter\b/g;

/** parse reads the commands and the size from a tape and settings.tape. */
export function parse(tape: string, settings: string) {
	const commands: string[] = [];
	const all: string[] = [];
	const size: Record<string, number> = {};
	let hidden = false;
	let typed = '';
	for (const line of `${settings}\n${tape}`.split('\n').map((l) => l.trim())) {
		if (line === '' || line.startsWith('#')) continue;
		if (line === 'Hide' || line === 'Show') {
			hidden = line === 'Hide';
			continue;
		}
		const set = line.match(/^Set (Width|Height) (\d+)$/);
		if (set) size[set[1]] = Number(set[2]);
		for (const m of line.matchAll(typeOrEnter)) {
			if (m[0] !== 'Enter') {
				typed += m[1] ?? m[2] ?? m[3];
				continue;
			}
			all.push(typed);
			if (!hidden) commands.push(typed);
			typed = '';
		}
	}
	const width = size.Width ?? 0;
	const height = size.Height ?? 0;
	return { commands, all, width, height, cols: Math.round((width - 118) / 16.8) };
}

/** hash names a tape's recording: it changes with the tape, its look, and each release. */
export function hash(name: string): string {
	return createHash('sha256')
		.update(read(`${name}.tape`))
		.update(read('settings.tape'))
		.update(read('theme.json'))
		.update(plugin.version)
		.digest('hex')
		.slice(0, 12);
}

export function loadTape(name: string): Tape {
	const { commands, all, width, height, cols } = parse(read(`${name}.tape`), read('settings.tape'));
	const file = `${mediaBase}/${name}-${hash(name)}`;
	return { commands, all, width, height, cols, mp4: `${file}.mp4`, png: `${file}.png`, txt: `${file}.txt` };
}

/** transcript fetches a recording's last frame as text, and fails the build if it is missing. */
export async function transcript(name: string, tape: Tape): Promise<string> {
	const res = await fetch(tape.txt);
	if (!res.ok) {
		throw new Error(
			`The ${name} tape is not recorded (${res.status} for ${tape.txt}). Run: site/scripts/record.sh`,
		);
	}
	return (await res.text()).replace(/\s+$/, '');
}
